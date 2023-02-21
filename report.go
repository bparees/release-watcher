package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"time"

	"k8s.io/klog"
)

func generateReport(releaseAPIUrl string, acceptedStalenessLimit, builtStalenessLimit time.Duration, oldestMinor int) (string, error) {
	acceptedReleases, err := getReleaseStream(releaseAPIUrl + acceptedReleasePath)
	if err != nil {
		return "", fmt.Errorf("Error fetching releases from %s: %v\n", releaseAPIUrl+acceptedReleasePath, err)

	}
	allReleases, err := getReleaseStream(releaseAPIUrl + allReleasePath)
	if err != nil {
		return "", fmt.Errorf("Error fetching releases from %s: %v\n", releaseAPIUrl+allReleasePath, err)
	}

	acceptedEmpty, acceptedStale := getEmptyAndStaleStreams(acceptedReleases, acceptedStalenessLimit, oldestMinor)
	allEmpty, allStale := getEmptyAndStaleStreams(allReleases, acceptedStalenessLimit, oldestMinor)

	report := ""
	for stream, _ := range acceptedEmpty {
		// if there are no accepted payloads, but the overall payloads set for the stream is not empty
		// (and especially if the overall payloads are not stale), flag it.  If the overall stream is empty,
		// we'll flag it further below.
		if _, ok := allStale[stream]; !ok {
			report += fmt.Sprintf("Release stream %s has no accepted payloads, but the stream contains recently built payloads: "+releaseStreamUrl+"\n", stream, stream)
		} else if _, ok := allEmpty[stream]; !ok {
			report += fmt.Sprintf("Release stream %s has no accepted payloads, but the stream contains built payloads: "+releaseStreamUrl+"\n", stream, stream)
		}

	}
	for stream, age := range acceptedStale {
		// if the latest accepted payload is stale, but there are non-stale payloads that have been built,
		// flag it.  If the overall stream is stale(no recently built payloads), we'll flag it elsewhere.
		if _, ok := allStale[stream]; !ok {
			report += fmt.Sprintf("Release stream %s most recently accepted payload was %.1f days ago, latest built payload is < %.1f days old: "+releaseStreamUrl+"\n", stream, age.Hours()/24, builtStalenessLimit.Hours()/24, stream)
		}
	}

	for _, s := range allEmpty {
		report += fmt.Sprintf("Release stream %s has no built payloads: "+releaseStreamUrl+"\n", s, s)
	}

	_, allVeryStale := getEmptyAndStaleStreams(allReleases, builtStalenessLimit, oldestMinor)

	for k, v := range allVeryStale {
		report += fmt.Sprintf("Release stream %s most recently built payload was %.1f days ago: "+releaseStreamUrl+"\n", k, v.Hours()/24, k)
	}
	return report, nil
}

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

func getEmptyAndStaleStreams(releases map[string][]string, threshold time.Duration, oldestMinor int) (map[string]struct{}, map[string]time.Duration) {
	emptyStreams := make(map[string]struct{})
	staleStreams := make(map[string]time.Duration)
	releaseKeys := reflect.ValueOf(releases).MapKeys()
	now := time.Now()
	for _, k := range releaseKeys {
		stream := k.String()

		matches := zReleaseRegex.FindStringSubmatch(stream)

		if matches == nil {
			//fmt.Printf("ignoring non z-stream release %s\n", stream)
			continue
		}
		if v, _ := strconv.Atoi(matches[1]); v < oldestMinor {
			klog.V(4).Infof("ignoring release %s because it is older than the oldest desired minor %d\n", stream, oldestMinor)
			continue
		}
		if len(releases[stream]) == 0 {
			emptyStreams[stream] = struct{}{}
			continue
		}
		freshPayload := false
		var newest time.Time
		for _, payload := range releases[stream] {
			m := extractDateRegex.FindStringSubmatch(payload)
			if m == nil || len(m) != 7 {
				klog.Errorf("error: could not extract date from payload %s in stream %s\n", payload, stream)
				continue
			}
			//fmt.Printf("Release %s has date %s\n", r, m[0])
			//t := time.Date(m[1], m[2], m[3], m[4], m[5], m[6], 0, time.UTC)
			payloadTime, err := time.Parse("2006-01-02-150405 MST", m[0]+" EST")
			if err != nil {
				klog.Errorf("error: failed to parse time string %s: %v", m[0], err)
				continue
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
