package main

import (
	"flag"
	"fmt"
	"regexp"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/klog"
)

const (
	baseReleaseAPIUrl   = "https://amd64.ocp.releases.ci.openshift.org/api/v1"
	acceptedReleasePath = "/releasestreams/accepted"
	allReleasePath      = "/releasestreams/all"
	releaseStreamUrl    = "https://amd64.ocp.releases.ci.openshift.org/#%s"
)

var (
	// match these two formats:
	// 4.NNN.0-0.ci
	// 4.NNN.0-0.nightly
	zReleaseRegex = regexp.MustCompile(`4\.([1-9][0-9]*)\.0-0\.(ci|nightly)`)
	//extractMinorRegex = regexp.MustCompile(`4\.([1-9][0-9]*)\.0`)
	// YYYY-MM-DD-HHMMSS
	extractDateRegex = regexp.MustCompile(`([0-9]{4})-([0-9]{2})-([0-9]{2})-([0-9]{2})([0-9]{2})([0-9]{2})$`)
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
	releaseAPIUrl          string
	oldestMinor            int
	slackAlias             string
	acceptedStalenessLimit time.Duration
	builtStalenessLimit    time.Duration
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

	flagset := root.Flags()

	flagset.AddGoFlagSet(flag.CommandLine)

	flagset.AddGoFlag(original.Lookup("v"))

	if err := root.Execute(); err != nil {
		klog.Exitf("error: %v", err)
	}
}

func newReportCommand() *cobra.Command {
	o := &options{
		releaseAPIUrl: baseReleaseAPIUrl,
	}
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
	flagset.StringVar(&o.releaseAPIUrl, "release-api-url", o.releaseAPIUrl, "The url of the release reporting api")
	flagset.IntVar(&o.oldestMinor, "oldest-minor", 8, "The oldest minor release to analyze.  Release streams older than this will be ignored.  Specify only the minor value (e.g. \"13\")")
	flagset.DurationVar(&o.acceptedStalenessLimit, "accepted-staleness-limit", 24*time.Hour, "How old an accepted payload can be before it is considered stale, in hours")
	flagset.DurationVar(&o.builtStalenessLimit, "built-staleness-limit", 72*time.Hour, "How old an built payload can be before it is considered stale, in hours")

	return cmd
}

func newBotCommand() *cobra.Command {
	o := &options{
		releaseAPIUrl: baseReleaseAPIUrl,
	}
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
	flagset.StringVar(&o.releaseAPIUrl, "release-api-url", o.releaseAPIUrl, "The url of the release reporting api")
	flagset.IntVar(&o.oldestMinor, "oldest-minor", 8, "The oldest minor release to analyze.  Release streams older than this will be ignored.  Specify only the minor value (e.g. \"13\")")
	flagset.DurationVar(&o.acceptedStalenessLimit, "accepted-staleness-limit", 24*time.Hour, "How old an accepted payload can be before it is considered stale, in hours")
	flagset.DurationVar(&o.builtStalenessLimit, "built-staleness-limit", 72*time.Hour, "How old an built payload can be before it is considered stale, in hours")

	return cmd
}

func (o *options) runReport() error {
	report, err := generateReport(o.releaseAPIUrl, o.acceptedStalenessLimit, o.builtStalenessLimit, o.oldestMinor)
	if err != nil {
		return err
	}
	fmt.Println(report)
	return nil
}

func (o *options) runBot() error {
	o.serve()
	return nil
}
