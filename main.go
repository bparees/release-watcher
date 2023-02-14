package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"regexp"
	"time"
)

const (
	acceptedReleaseUrl = "https://amd64.ocp.releases.ci.openshift.org/api/v1/releasestreams/accepted"
	allReleaseUrl      = "https://amd64.ocp.releases.ci.openshift.org/api/v1/releasestreams/all"
	releaseStreamUrl   = "https://amd64.ocp.releases.ci.openshift.org/#%s"
)

var (
	// match these two formats:
	// 4.NNN.0-0.ci
	// 4.NNN.0-0.nightly
	zReleaseRegex = regexp.MustCompile(`4\.[1-9][0-9]*\.0-0\.(ci|nightly)`)
	// YYYY-MM-DD-HHMMSS
	extractDateRegex = regexp.MustCompile(`([0-9]{4})-([0-9]{2})-([0-9]{2})-([0-9]{2})([0-9]{2})([0-9]{2})$`)
)

func getReleaseStream(url string) (map[string][]string, error) {
	res, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("error fetching releases: %s", err)
	}
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("non-OK http response code: %d", res.StatusCode)
	}

	releases := make(map[string][]string)

	err = json.NewDecoder(res.Body).Decode(&releases)
	if err != nil {
		return nil, fmt.Errorf("error decoding releases: %v", err)
	}

	return releases, nil
}

func getStaleStreams(releases map[string][]string, threshold time.Duration) (map[string]struct{}, map[string]time.Duration) {
	emptyStreams := make(map[string]struct{})
	staleStreams := make(map[string]time.Duration)
	releaseKeys := reflect.ValueOf(releases).MapKeys()
	now := time.Now()
	for _, k := range releaseKeys {
		stream := k.String()
		if !zReleaseRegex.MatchString(stream) {
			//fmt.Printf("ignoring non z-stream release %s\n", stream)
			continue
		}
		if len(releases[stream]) == 0 {
			fmt.Printf("Release %s has no payloads!\n", stream)
			emptyStreams[stream] = struct{}{}
			continue
		}
		freshPayload := false
		var newest time.Time
		for _, payload := range releases[stream] {
			m := extractDateRegex.FindStringSubmatch(payload)
			if m == nil || len(m) != 7 {
				fmt.Printf("Error: could not extract date from payload %s in stream %s\n", payload, stream)
			}
			//fmt.Printf("Release %s has date %s\n", r, m[0])
			//t := time.Date(m[1], m[2], m[3], m[4], m[5], m[6], 0, time.UTC)
			payloadTime, err := time.Parse("2006-01-02-150405 MST", m[0]+" EST")
			if err != nil {
				fmt.Printf("Error parsing time string %s: %v", m[0], err)
			}
			//fmt.Printf("%v\n", t)
			delta := now.Sub(payloadTime)
			if delta.Minutes() < threshold.Minutes() {
				//fmt.Printf("Release %s in stream %s is %d minutes old!\n", r, stream, delta)
				freshPayload = true
			}
			if payloadTime.After(newest) {
				newest = payloadTime
			}
		}
		if !freshPayload {
			//fmt.Printf("Release stream %s does not have a recent payload: "+releaseStreamUrl+"\n", stream, stream)
			staleStreams[stream] = now.Sub(newest)
		}
	}
	return emptyStreams, staleStreams
}

// TODO
// add arguments:
//   args:
//     release stream api url
//     oldest minor version to care about
//     channel/alias to notify in report
// Sort/format report with sections/headers and sort by release version?
// What to do with the case: recent builds are newer than a week, but older than a day, so there
//   will be no recently accepted payload expected, but it also won't be reported as a stale build stream
// Just ignore them?  (If there are no accepted payloads period, it will still be flagged)

// What we do report:
//   accepted payload is older than a day when newer builds exist in the stream - we are failing to accept payloads regularly/may have regressed
//   no accepted builds in the stream when builds exist in the stream - we are completely failing to accept payloads, DIRE
//   no builds exist in the stream - either there have been no changes in the code(ok) or our build system is broken (not ok).  - ????
//   no build newer than a week exists in the stream - either there have been no changes in the code(ok) or our build system is broken (not ok).  - ????

func main() {
	acceptedReleases, err := getReleaseStream(acceptedReleaseUrl)
	if err != nil {
		fmt.Printf("Error fetching releases from %s: %v\n", acceptedReleaseUrl, err)
		os.Exit(1)
	}
	allReleases, err := getReleaseStream(allReleaseUrl)
	if err != nil {
		fmt.Printf("Error fetching releases from %s: %v\n", acceptedReleaseUrl, err)
		os.Exit(1)
	}

	acceptedStalenessThreshold := 24 * time.Hour
	builtStalenessThreshold := 24 * time.Hour
	acceptedEmpty, acceptedStale := getStaleStreams(acceptedReleases, acceptedStalenessThreshold)
	allEmpty, allStale := getStaleStreams(allReleases, builtStalenessThreshold)

	for stream, _ := range acceptedEmpty {
		// if there are no accepted payloads, but the overall payloads set for the stream is not empty
		// (and especially if the overall payloads are not stale), flag it.  If the overall stream is empty,
		// we'll flag it further below.
		if _, ok := allStale[stream]; !ok {
			fmt.Printf("Release stream %s has no accepted payloads, but the stream contains recently built payloads: "+releaseStreamUrl+"\n", stream, stream)
		} else if _, ok := allEmpty[stream]; !ok {
			fmt.Printf("Release stream %s has no accepted payloads, but the stream contains built payloads: "+releaseStreamUrl+"\n", stream, stream)
		}

	}
	for stream, age := range acceptedStale {
		// if the latest accepted payload is stale, but there are non-stale payloads that have been built,
		// flag it.  If the overall stream is stale, we'll flag it further below.
		if _, ok := allStale[stream]; !ok {
			fmt.Printf("Release stream %s most recently accepted payload was %.0f hours ago, latest built payload is < %.0f hours old: "+releaseStreamUrl+"\n", stream, age.Hours(), builtStalenessThreshold.Hours(), stream)
		}
	}

	for _, s := range allEmpty {
		fmt.Printf("Release stream %s has no built payloads: "+releaseStreamUrl+"\n", s, s)
	}

	_, allVeryStale := getStaleStreams(allReleases, 7*24*time.Hour)

	for k, v := range allVeryStale {
		fmt.Printf("Release stream %s most recently built payload was %.0f hours ago: "+releaseStreamUrl+"\n", k, v.Truncate(time.Hour).Hours(), k)
	}

	/*
		staleStreams := make(map[string]time.Duration)
		allReleaseKeys := reflect.ValueOf(allReleases).MapKeys()
		for _, k := range allReleaseKeys {
			stream := k.String()
			if !zReleaseRegex.MatchString(stream) {
				//fmt.Printf("ignoring non z-stream release %s\n", stream)
				continue
			}
			if len(allReleases[stream]) == 0 {
				fmt.Printf("Release %s has no payloads!\n", stream)
				continue
			}
			now := time.Now()

			freshPayload := false
			var newest time.Time
			for _, payload := range allReleases[stream] {
				m := extractDateRegex.FindStringSubmatch(r)
				if m == nil || len(m) != 7 {
					fmt.Printf("Error: could not extract date from payload %s in stream %s\n", payload, stream)
				}
				//fmt.Printf("Release %s has date %s\n", r, m[0])
				//t := time.Date(m[1], m[2], m[3], m[4], m[5], m[6], 0, time.UTC)
				payloadTime, err := time.Parse("2006-01-02-150405 MST", m[0]+" EST")
				if err != nil {
					fmt.Printf("Error parsing time string %s: %v", m[0], err)
				}
				//fmt.Printf("%v\n", t)
				delta := now.Sub(payloadTime)
				if delta.Minutes() < 24*60 {
					//fmt.Printf("Release %s in stream %s is %d minutes old!\n", r, stream, delta)
					freshPayload = true
				}
				if payloadTime.After(newest) {
					newest = payloadTime
				}
			}
			if !freshPayload {
				fmt.Printf("Release stream %s does not have a recent payload: "+releaseStreamUrl+"\n", stream, stream)
				staleStreams[stream] = now.Sub(newest)
			}
		}

		acceptedReleaseKeys := reflect.ValueOf(acceptedReleases).MapKeys()
		for _, k := range acceptedReleaseKeys {
			stream := k.String()
			if !zReleaseRegex.MatchString(stream) {
				//fmt.Printf("ignoring non z-stream release %s\n", stream)
				continue
			}
			if len(acceptedReleases[stream]) == 0 {
				fmt.Printf("Release %s has no accepted payloads!\n", stream)
				continue
			}
			now := time.Now()

			freshPayload := false
			for _, r := range acceptedReleases[stream] {
				m := extractDateRegex.FindStringSubmatch(r)
				if m == nil || len(m) != 7 {
					fmt.Printf("Error: could not extract date from release %s in stream %s\n", r, stream)
				}
				//fmt.Printf("Release %s has date %s\n", r, m[0])
				//t := time.Date(m[1], m[2], m[3], m[4], m[5], m[6], 0, time.UTC)
				releaseTime, err := time.Parse("2006-01-02-150405 MST", m[0]+" EST")
				if err != nil {
					fmt.Printf("Error parsing time string %s: %v", m[0], err)
				}
				//fmt.Printf("%v\n", t)
				delta := now.Sub(releaseTime)
				if delta.Minutes() < 24*60 {
					//fmt.Printf("Release %s in stream %s is %d minutes old!\n", r, stream, delta)
					freshRelease = true
				}
			}
			if !freshRelease {
				fmt.Printf("Release stream %s does not have a recent accepted release: "+releaseStreamUrl+"\n", stream, stream)
			}

		}
	*/

}
