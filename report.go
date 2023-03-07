package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"time"

	"k8s.io/klog"
)

func generateReport(releaseAPIUrl string, acceptedStalenessLimit, builtStalenessLimit, upgradeStalenessLimit time.Duration, oldestMinor, newestMinor int, includeHealthy bool) (string, error) {
	acceptedReleases, err := getReleaseStream(releaseAPIUrl + acceptedReleasePath)
	if err != nil {
		return "", err

	}
	allReleases, err := getReleaseStream(releaseAPIUrl + allReleasePath)
	if err != nil {
		return "", err
	}

	// stable graph only includes successful edges.  nightly+prerelease include edges for any upgrade attempt that was
	// made, regardless of whether the job passed.
	nightlyGraph, err := getUpgradeGraph("https://amd64.ocp.releases.ci.openshift.org", "stable")
	if err != nil {
		return "", err
	}

	/*
		 prereleaseGraph, err := getUpgradeGraph("https://amd64.ocp.releases.ci.openshift.org", "prerelease")
		if err != nil {
			return "", err
		}
	*/

	//report := checkUpgrades(nightlyGraph, acceptedReleases, acceptedStalenessLimit, oldestMinor)
	report := checkUpgrades(nightlyGraph, allReleases, upgradeStalenessLimit, oldestMinor, newestMinor, includeHealthy)

	acceptedEmpty, acceptedStale := getEmptyAndStaleStreams(acceptedReleases, acceptedStalenessLimit, oldestMinor, newestMinor)
	allEmpty, allStale := getEmptyAndStaleStreams(allReleases, acceptedStalenessLimit, oldestMinor, newestMinor)

	for stream, _ := range acceptedEmpty {
		// if there are no accepted payloads, but the overall payloads set for the stream is not empty
		// (and especially if the overall payloads are not stale), flag it.  If the overall stream is empty,
		// we'll flag it further below.
		if _, ok := allStale[stream]; !ok {
			report[stream] = append(report[stream], "Has no accepted payloads, but the stream contains recently built payloads")
		} else if _, ok := allEmpty[stream]; !ok {
			report[stream] = append(report[stream], "Has no accepted payloads, but the stream contains built payloads")
		}

	}
	for stream, age := range acceptedStale {
		// if the latest accepted payload is stale, but there are non-stale payloads that have been built,
		// flag it.  If the overall stream is stale(no recently built payloads), we'll flag it elsewhere.
		if _, ok := allStale[stream]; !ok {
			report[stream] = append(report[stream], fmt.Sprintf("Most recently accepted payload was %.1f days ago, latest built payload is < %.1f days old", age.Hours()/24, acceptedStalenessLimit.Hours()/24))
		}
	}

	for stream, _ := range allEmpty {
		report[stream] = append(report[stream], "Has no built payloads")
	}

	_, allVeryStale := getEmptyAndStaleStreams(allReleases, builtStalenessLimit, oldestMinor, newestMinor)

	for stream, age := range allVeryStale {
		report[stream] = append(report[stream], fmt.Sprintf("Most recently built payload was %.1f days ago", age.Hours()/24))
	}

	streams := []string{}
	for stream, _ := range report {
		streams = append(streams, stream)
	}

	sort.Strings(streams)
	sort.Slice(streams, func(i, j int) bool {
		iMatches := extractMinorRegex.FindStringSubmatch(streams[i])
		iVersion, _ := strconv.Atoi(iMatches[1])
		jMatches := extractMinorRegex.FindStringSubmatch(streams[j])
		jVersion, _ := strconv.Atoi(jMatches[1])
		// this deliberately reverses the standard sorting order so we
		// get highest to lowest.
		return iVersion > jVersion

	})

	output := ""

	for _, stream := range streams {
		output += fmt.Sprintf(releaseStreamUrl+"\n", stream)
		for _, o := range report[stream] {
			output += fmt.Sprintf("  - %s\n", o)
		}
		output += "\n"
	}
	output += fmt.Sprintf("\nIgnored releases older than 4.%d.z and newer than 4.%d.z\n", oldestMinor, newestMinor)
	return output, nil
}

func getReleaseStream(url string) (map[string][]string, error) {
	res, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("error fetching releases from %s: %s", url, err)
	}
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("non-OK http response code from %s: %d", url, res.StatusCode)
	}

	releases := make(map[string][]string)

	err = json.NewDecoder(res.Body).Decode(&releases)
	if err != nil {
		return nil, fmt.Errorf("error decoding releases from %s: %v", url, err)
	}

	return releases, nil
}

func getEmptyAndStaleStreams(releases map[string][]string, threshold time.Duration, oldestMinor, newestMinor int) (map[string]struct{}, map[string]time.Duration) {
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
		if v, _ := strconv.Atoi(matches[1]); v > newestMinor {
			klog.V(4).Infof("ignoring release %s because it is newer than the newest desired minor %d\n", stream, newestMinor)
			continue
		}
		if len(releases[stream]) == 0 {
			emptyStreams[stream] = struct{}{}
			continue
		}
		freshPayload := false
		var newest time.Time
		for _, payload := range releases[stream] {
			ts, err := getPayloadTimestamp(payload)
			if err != nil {
				klog.Errorf(err.Error())
				continue
			}
			delta := now.Sub(ts)
			if delta.Minutes() < threshold.Minutes() {
				//fmt.Printf("Release %s in stream %s is %d minutes old!\n", r, stream, delta)
				freshPayload = true
			}
			if ts.After(newest) {
				newest = ts
			}
		}
		if !freshPayload {
			//fmt.Printf("Release stream %s does not have a recent payload: "+releaseStreamUrl+"\n", stream, stream)
			staleStreams[stream] = now.Sub(newest)
		}
	}
	return emptyStreams, staleStreams
}

func getPayloadTimestamp(payload string) (time.Time, error) {
	m := extractDateRegex.FindStringSubmatch(payload)
	if m == nil || len(m) != 7 {
		return time.Time{}, fmt.Errorf("error: could not extract date from payload %s", payload)
	}
	//fmt.Printf("Release %s has date %s\n", r, m[0])
	//t := time.Date(m[1], m[2], m[3], m[4], m[5], m[6], 0, time.UTC)
	payloadTime, err := time.Parse("2006-01-02-150405 MST", m[0]+" EST")
	if err != nil {
		return time.Time{}, fmt.Errorf("error: failed to parse time string %s: %v", m[0], err)
	}
	//fmt.Printf("%v\n", t)
	return payloadTime, nil

}

type GraphNode struct {
	Version string `json:"version"`
	Payload string `json:"payload"`
	From    int
}

type GraphEdge [2]int

type Graph struct {
	Nodes []GraphNode `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
}

type GraphMap map[string][]string

func getUpgradeGraph(apiurl, channel string) (GraphMap, error) {
	graphMap := GraphMap{}

	graph := Graph{}
	url := apiurl + "/graph?channel=" + channel
	res, err := http.Get(url)
	if err != nil {
		return graphMap, fmt.Errorf("error fetching upgrade graph from %s: %s", url, err)
	}
	if res.StatusCode != 200 {
		return graphMap, fmt.Errorf("non-OK http response code fetching upgrade graph from %s: %d", url, res.StatusCode)
	}

	err = json.NewDecoder(res.Body).Decode(&graph)
	if err != nil {
		return graphMap, fmt.Errorf("error decoding upgrade graph: %v", err)
	}

	for _, edge := range graph.Edges {
		from := edge[0]
		to := edge[1]
		graph.Nodes[to].From = from
		if _, ok := graphMap[graph.Nodes[to].Version]; !ok {
			graphMap[graph.Nodes[to].Version] = []string{graph.Nodes[from].Version}
		} else {
			graphMap[graph.Nodes[to].Version] = append(graphMap[graph.Nodes[to].Version], graph.Nodes[from].Version)
		}
	}

	return graphMap, nil
}

type found struct {
	Version string
	Age     time.Duration
}

func (f *found) Days() float64 {
	return f.Age.Hours() / 24
}

func checkUpgrades(graph GraphMap, releases map[string][]string, stalenessThreshold time.Duration, oldestMinor, newestMinor int, includeHealthy bool) map[string][]string {
	report := make(map[string][]string)
	now := time.Now()
	for release, payloads := range releases {

		matches := zReleaseRegex.FindStringSubmatch(release)

		if matches == nil {
			klog.V(4).Infof("not checking upgrade status for non z-stream release %s", release)
			continue
		}
		v, _ := strconv.Atoi(matches[1])
		if v < oldestMinor {
			klog.V(4).Infof("ignoring release %s because it is older than the oldest desired minor %d\n", release, oldestMinor)
			continue
		}
		if v > newestMinor {
			klog.V(4).Infof("ignoring release %s because it is newer than the newest desired minor %d\n", release, newestMinor)
			continue
		}

		var foundMinor *found
		var foundPatch *found
		for _, payload := range payloads {
			ts, err := getPayloadTimestamp(payload)
			if err != nil {
				klog.Error(err.Error())
				continue
			}
			age := now.Sub(ts)
			if age.Minutes() > stalenessThreshold.Minutes() {
				continue
			}
			toMatches := extractMinorRegex.FindStringSubmatch(payload)
			if toMatches == nil {
				continue
			}
			toVersion, _ := strconv.Atoi(toMatches[1])

			for _, from := range graph[payload] {

				fromMatches := extractMinorRegex.FindStringSubmatch(from)

				if fromMatches == nil {
					klog.V(4).Infof("Ignoring upgrade to %s from %s because the minor version could not be determined\n", payload, from)
					continue
				}
				fromVersion, _ := strconv.Atoi(fromMatches[1])

				klog.V(4).Infof("Accepted payload %s upgrades from %s\n", payload, from)
				if toVersion == fromVersion {
					foundPatch = &found{
						Version: from,
						Age:     age,
					}
				}
				if toVersion == fromVersion+1 {
					foundMinor = &found{
						Version: from,
						Age:     age,
					}
				}
				if foundMinor != nil && foundPatch != nil {
					// we have found a recent payload in the set of accepted payloads this release, which successfully upgraded from a previous minor
					// and a previous patch, so we don't need to continue checking payloads for this release.
					break
				}
			}
		}

		if foundPatch == nil {
			report[release] = append(report[release], "Does not have a recent valid patch level upgrade")
		} else if includeHealthy {
			report[release] = append(report[release], fmt.Sprintf("Has a recent valid patch level upgrade from %s %0.1f days ago", foundPatch.Version, foundPatch.Days()))
		}
		if foundMinor == nil {
			report[release] = append(report[release], "Does not have a recent valid minor level upgrade")
		} else if includeHealthy {
			report[release] = append(report[release], fmt.Sprintf("Has a recent valid minor level upgrade from %s %0.1f days ago", foundMinor.Version, foundMinor.Days()))
		}
	}
	return report
}
