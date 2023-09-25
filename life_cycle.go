package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"k8s.io/klog"
)

type productLifeCycleResponse struct {
	Data []productLifeCycle `json:"data"`
}

type productLifeCycle struct {
	Name     string                    `json:"name"`
	Versions []productLifeCycleVersion `json:"versions"`
}

type productLifeCycleVersion struct {
	Name string `json:"name"`
	Type string `json:"type:`
}

func getSupportedReleases(url string) (int, int, error) {
	resp, err := http.Get(url)
	if err != nil {
		return 0, 0, fmt.Errorf("error fetching life-cycle data from %s: %s", url, err)
	}
	if resp.StatusCode != 200 {
		return 0, 0, fmt.Errorf("non-OK http response code from %s: %d", url, resp.StatusCode)
	}
	defer resp.Body.Close()

	data := productLifeCycleResponse{}
	if err = json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return 0, 0, fmt.Errorf("error decoding life-cycle data from %s: %s", url, err)
	}

	if len(data.Data) != 1 {
		return 0, 0, fmt.Errorf("life-cycle data from %s contains %d products, but should only contain 1", url)
	}

	minSupportedRelease := -1
	maxSupportedRelease := -1
	for _, version := range data.Data[0].Versions {
		if version.Type == "End of life" {
			continue
		}
		entries := strings.Split(version.Name, ".")
		if len(entries) != 2 {
			klog.V(4).Infof("expected one period in %q for parsing a minor version", version.Name)
			continue
		}
		if entries[0] != "4" {
			klog.V(4).Infof("expected major version 4 in %q, not %q", version.Name, entries[0])
			continue
		}

		minor, err := strconv.Atoi(entries[1])
		if err != nil {
			klog.V(4).Infof("expected integer minor version in %q, not %q: %v", version.Name, entries[1], err)
			continue
		}

		if minSupportedRelease == -1 || minSupportedRelease > minor {
			minSupportedRelease = minor
		}
		if maxSupportedRelease == -1 || maxSupportedRelease < minor {
			maxSupportedRelease = minor
		}
	}

	if minSupportedRelease == -1 {
		return 0, 0, fmt.Errorf("life-cycle data from %s contains no supported releases for %s", url, data.Data[0].Name)
	}

	return minSupportedRelease, maxSupportedRelease, nil
}
