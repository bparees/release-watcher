package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"k8s.io/klog"
)

var (
	mutex          = &sync.Mutex{}
	msgCache       = make(map[string]struct{})
	auth_token     string
	patchmanagerId = "U9ARYTT7Z"
)

type Request struct {
	Token string `json:"token"`
	Type  string `json:"type"`

	// challenge request fields
	Challenge string `json:"challenge"`

	// events
	Event Event `json:"event"`
}

type Event struct {
	Type    string `json:"type"`
	Text    string `json:"text"`
	User    string `json:"user"`
	Channel string `json:"channel"`
	TS      string `json:"ts"`
}

type VerificationResponse struct {
	Challenge string `json:"challenge"`
}

type PostMessage struct {
	Token   string `json:"token"`
	Channel string `json:"channel"`
	Text    string `json:"text"`
}

func (o *options) serve() {
	rand.Seed(time.Now().UTC().UnixNano())
	auth_token = os.Getenv("TOKEN")
	http.HandleFunc("/", o.createHandler())  // set router
	err := http.ListenAndServe(":8080", nil) // set listen port
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}

func (o *options) createHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := ioutil.ReadAll(r.Body)
		if err != nil {
			fmt.Printf("error: %v\n", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		req := Request{}
		if err := json.Unmarshal([]byte(body), &req); err != nil {
			fmt.Printf("error: %v\n", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		//fmt.Printf("struct: %#v", req)
		if req.Type == "url_verification" {
			resp := VerificationResponse{Challenge: req.Challenge}
			w.Header().Set("Content-type", "application/json")
			w.WriteHeader(http.StatusOK)
			respJson, _ := json.Marshal(resp)
			io.WriteString(w, string(respJson))
			return
		}

		if req.Type == "event_callback" {

			mutex.Lock()
			if _, found := msgCache[req.Event.TS]; found {
				klog.V(4).Infof("ignoring dupe event: %#v\n", req.Event)
				w.WriteHeader(http.StatusOK)
				mutex.Unlock()
				return
			}
			msgCache[req.Event.TS] = struct{}{}
			mutex.Unlock()
			klog.V(4).Infof("saw message event: %#v\n", req.Event)

			msg := PostMessage{}
			msg.Channel = req.Event.Channel

			switch {
			case strings.Contains(req.Event.Text, "help"):
				msg.Text = fmt.Sprintf(`help - help
report - Generates human reports about which release streams do not have recently built or recently accepted payloads, based on the release info found at https://amd64.ocp.releases.ci.openshift.org/
Current arguments:
  Accepted payloads must be newer than %0.1f hours
  Payloads must have been built within the last %0.1f hours
  Ignoring releases older than 4.%d`, o.acceptedStalenessLimit.Hours(), o.builtStalenessLimit.Hours(), o.oldestMinor)
			case strings.Contains(req.Event.Text, "report"):
				msg.Text, err = generateReport(o.releaseAPIUrl, o.acceptedStalenessLimit, o.builtStalenessLimit, o.upgradeStalenessLimit, o.oldestMinor, o.newestMinor)
				if err != nil {
					msg.Text = fmt.Sprintf("Sorry, an error occurred generating the report: %v", err)
				}
			default:
				msg.Text = fmt.Sprintf("Sorry, I couldn't process that request: %s", req.Event.Text)
			}

			// never output our own name, so we don't trigger ourselves
			//fmt.Printf("original response: %s\n", msg.Text)
			msg.Text = strings.Replace(msg.Text, "@UE23Q9BFY", "OCP Payload Reporter", -1)
			//fmt.Printf("replaced response: %s\n", msg.Text)

			msgJson, _ := json.Marshal(msg)

			fmt.Printf("msg response json: %s\n", msgJson)
			req, err := http.NewRequest("POST", "https://slack.com/api/chat.postMessage", bytes.NewBuffer(msgJson))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", auth_token))

			client := &http.Client{}
			resp, err := client.Do(req)
			if err != nil {
				fmt.Printf("error posting chat message: %v", err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			//fmt.Printf("chat message response: %#v\n", resp)
			resp.Body.Close()

			w.WriteHeader(http.StatusOK)
			//respJson, _ := json.Marshal(resp)
			//io.WriteString(w, string(respJson))
		}
	}
}
