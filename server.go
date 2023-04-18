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
	"strconv"
	"strings"
	"sync"
	"time"

	"k8s.io/klog"
)

var (
	mutex          = &sync.Mutex{}
	msgCache       = make(map[string]struct{})
	auth_token     string
	patchmanagerId = "SMZ7PJ1L0"
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
	Token    string `json:"token"`
	Channel  string `json:"channel"`
	Text     string `json:"text"`
	ThreadTS string `json:"thread_ts,omitempty"`
}

type PostMessageResponse struct {
	TS string `json:"ts"`
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

			msg := ""
			switch {
			case strings.Contains(req.Event.Text, "help"):
				sendMessage(fmt.Sprintf(`help - help
report - Generates human reports about which release streams do not have recently built or recently accepted payloads, based on the release info found at https://amd64.ocp.releases.ci.openshift.org/
Arguments:
  min=X - only look at z-streams with a minimum version of X, e.g. min=9
  max=X - only look at z-streams with a maximum version of X, e.g. max=12
  healthy - include healthy z-streams in the report
  tag - tag patch manager with the report output
Current settings:
  Accepted payloads must be newer than %0.1f hours
  Payloads must have been built within the last %0.1f hours
  Ignoring releases older than 4.%d and newer than 4.%d`, o.acceptedStalenessLimit.Hours(), o.builtStalenessLimit.Hours(), o.oldestMinor, o.newestMinor),
					req.Event.Channel, "")
			case strings.Contains(req.Event.Text, "report"):
				reportOptions := *o
				reportOptions.includeHealthy = false
				tagPatchManager := false

				args := strings.Split(req.Event.Text, " ")
				for _, arg := range args {
					if arg == "tag" {
						tagPatchManager = true
					}

					if arg == "healthy" {
						reportOptions.includeHealthy = true
					}
					if strings.Contains(arg, "=") {
						v := strings.Split(arg, "=")
						switch v[0] {
						case "min":
							i, err := strconv.Atoi(v[1])
							if err != nil {
								sendMessage(fmt.Sprintf("Error parsing min z-stream version value %q: %s", v[1], err), req.Event.Channel, "")
								return
							}
							reportOptions.oldestMinor = i

						case "max":
							i, err := strconv.Atoi(v[1])
							if err != nil {
								sendMessage(fmt.Sprintf("Error parsing max z-stream version value %q: %s", v[1], err), req.Event.Channel, "")
								return
							}
							reportOptions.newestMinor = i
						}
					}

				}

				msg, err = generateReport(reportOptions.releaseAPIUrl, reportOptions.acceptedStalenessLimit, reportOptions.builtStalenessLimit, reportOptions.upgradeStalenessLimit, reportOptions.oldestMinor, reportOptions.newestMinor, reportOptions.includeHealthy)
				if err != nil {
					msg = fmt.Sprintf("Sorry, an error occurred generating the report: %v", err)
				}
				if tagPatchManager {
					if reportOptions.includeHealthy {
						msg = fmt.Sprintf("<!subteam^%s> here is the latest payload health report\n\n%s", patchmanagerId, msg)
					} else {
						msg = fmt.Sprintf("<!subteam^%s> here are the currently unhealthy payload streams that need investigation:\n\n%s", patchmanagerId, msg)
					}
				}

			default:
				msg = fmt.Sprintf("Sorry, I couldn't process that request: %s", req.Event.Text)
			}

			ts, err := sendMessage("Latest payload stream health report thread", req.Event.Channel, "")
			if err != nil {
				return
			}
			_, err = sendMessage(msg, req.Event.Channel, ts)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			} else {
				w.WriteHeader(http.StatusOK)
			}
		}
	}
}

func sendMessage(msg, channel, thread string) (string, error) {
	post := PostMessage{}
	post.Channel = channel
	// never output our own name, so we don't trigger ourselves
	//fmt.Printf("original response: %s\n", msg.Text)
	post.Text = strings.Replace(msg, "@UE23Q9BFY", "OCP Payload Reporter", -1)
	//fmt.Printf("replaced response: %s\n", msg.Text)

	if thread != "" {
		post.ThreadTS = thread
	}

	postJson, _ := json.Marshal(post)

	fmt.Printf("msg post json: %s\n", postJson)
	req, err := http.NewRequest("POST", "https://slack.com/api/chat.postMessage", bytes.NewBuffer(postJson))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", auth_token))

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("error posting chat message: %v", err)
		return "", err
	}
	// fmt.Printf("chat message response: %#v\n", resp)

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("error reading message response body: %v\n", err)
		return "", err
	}
	msgResp := PostMessageResponse{}
	if err := json.Unmarshal([]byte(body), &msgResp); err != nil {
		fmt.Printf("error reading message response body: %v\n", err)
		return "", err
	}
	resp.Body.Close()
	return msgResp.TS, nil
}
