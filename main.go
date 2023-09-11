package main

import (
	"flag"
	"fmt"
	"regexp"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/klog"
)

const (
	acceptedReleasePath = "/api/v1/releasestreams/accepted"
	allReleasePath      = "/api/v1/releasestreams/all"
)

var (
	// match these two formats:
	// 4.NNN.0-0.ci
	// 4.NNN.0-0.nightly
	zReleaseRegex     = regexp.MustCompile(`4\.([1-9][0-9]*)\.0-0\.(ci|nightly)`)
	extractMinorRegex = regexp.MustCompile(`4\.([1-9][0-9]*)\.[0-9]+`)
	// YYYY-MM-DD-HHMMSS
	extractDateRegex = regexp.MustCompile(`([0-9]{4})-([0-9]{2})-([0-9]{2})-([0-9]{2})([0-9]{2})([0-9]{2})$`)

	releaseAPIUrls = map[string]string{
		"amd64":   "https://amd64.ocp.releases.ci.openshift.org",
		"arm64":   "https://arm64.ocp.releases.ci.openshift.org",
		"multi":   "https://multi.ocp.releases.ci.openshift.org",
		"ppc64le": "https://ppc64le.ocp.releases.ci.openshift.org",
		"s390x":   "https://s390x.ocp.releases.ci.openshift.org",
	}
)

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

type options struct {
	oldestMinor            int
	newestMinor            int
	slackAlias             string
	acceptedStalenessLimit time.Duration
	builtStalenessLimit    time.Duration
	upgradeStalenessLimit  time.Duration
	includeHealthy         bool
	arch                   string
}

func main() {
	root := &cobra.Command{}
	root.AddCommand(
		newReportCommand(),
		newBotCommand(),
	)

	original := flag.CommandLine
	klog.InitFlags(original)
	original.Set("alsologtostderr", "true")
	original.Set("v", "2")

	root.PersistentFlags().AddGoFlag(original.Lookup("v"))
	if err := root.Execute(); err != nil {
		klog.Exitf("error: %v", err)
	}
}

func newReportCommand() *cobra.Command {
	o := &options{}
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Run a payload report and print the result to the command line",

		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return o.runReport()
		},
	}
	flagset := cmd.Flags()
	addSharedFlags(flagset, o)
	return cmd
}

func newBotCommand() *cobra.Command {
	o := &options{}
	cmd := &cobra.Command{
		Use:   "bot",
		Short: "Run the payload report bot server",

		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return o.runBot()
		},
	}

	flagset := cmd.Flags()
	flagset.StringVar(&o.slackAlias, "slack-alias", "", "Slack alias to tag in the generated report.  Leave empty to not tag anyone.")
	addSharedFlags(flagset, o)
	return cmd
}

func addSharedFlags(flagset *pflag.FlagSet, o *options) {
	flagset.IntVar(&o.oldestMinor, "oldest-minor", -1, "The oldest minor release to analyze.  Release streams older than this will be ignored.  Specify only the minor value (e.g. \"9\") (default to looking up the newest supported release)")
	flagset.IntVar(&o.newestMinor, "newest-minor", -1, "The newest minor release to analyze.  Release streams newer than this will be ignored.  Specify only the minor value (e.g. \"12\") (default to looking up the newest supported release)")
	flagset.DurationVar(&o.acceptedStalenessLimit, "accepted-staleness-limit", 24*time.Hour, "How old an accepted payload can be before it is considered stale")
	flagset.DurationVar(&o.builtStalenessLimit, "built-staleness-limit", 72*time.Hour, "How old an built payload can be before it is considered stale")
	flagset.DurationVar(&o.upgradeStalenessLimit, "upgrade-staleness-limit", 72*time.Hour, "How old a successful upgrade attempt can be before it's considered stale")
	flagset.BoolVar(&o.includeHealthy, "include-healthy", false, "Report about healthy payloads, not just failures")
	flagset.StringVar(&o.arch, "arch", "amd64", "Which architecture to report on (amd64, arm64)")
}

func (o *options) runReport() error {
	report, err := generateReport(o.acceptedStalenessLimit, o.builtStalenessLimit, o.upgradeStalenessLimit, o.oldestMinor, o.newestMinor, o.arch)
	if err != nil {
		return err
	}
	fmt.Println(report.String(o.includeHealthy))
	return nil
}

func (o *options) runBot() error {
	o.serve()
	return nil
}
