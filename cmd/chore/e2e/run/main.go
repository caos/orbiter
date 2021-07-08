package main

import (
	"context"
	"flag"
	"fmt"
	"math"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/caos/orbos/pkg/orb"

	"github.com/afiskon/promtail-client/promtail"
	"github.com/caos/orbos/internal/helpers"
)

func main() {

	var (
		settings                          programSettings
		from                              int
		graphiteURL, graphiteKey, lokiURL string
		returnCode                        int
	)

	defer func() {
		if err := recover(); err != nil {
			panic(err)
		}

		os.Exit(returnCode)
	}()

	const (
		orbDefault         = "~/.orb/config"
		orbUsage           = "Path to the orbconfig file which points to the orb the end-to-end testing should be performed on"
		graphiteURLDefault = ""
		graphiteURLUsage   = "https://<your-subdomain>.hosted-metrics.grafana.net/metrics"
		graphiteKeyDefault = ""
		graphiteKeyUsage   = "your api key from grafana.net -- should be editor role"
		lokiURLDefault     = ""
		lokiURLUsage       = "https://<instanceId>:<apiKey>@<instanceUrl>/api/prom/push"
		fromDefault        = 1
		fromUsage          = "step to continue e2e tests from"
		cleanupDefault     = true
		cleanupUsage       = "destroy orb after tests are done"
	)

	flag.StringVar(&settings.orbconfig, "orbconfig", orbDefault, orbUsage)
	flag.StringVar(&settings.orbconfig, "f", orbDefault, orbUsage+" (shorthand)")
	flag.StringVar(&graphiteURL, "graphiteurl", graphiteURLDefault, graphiteURLUsage)
	flag.StringVar(&graphiteURL, "g", graphiteURLDefault, graphiteURLUsage+" (shorthand)")
	flag.StringVar(&graphiteKey, "graphitekey", graphiteKeyDefault, graphiteKeyUsage)
	flag.StringVar(&graphiteKey, "k", graphiteKeyDefault, graphiteKeyUsage+" (shorthand)")
	flag.StringVar(&lokiURL, "lokiurl", lokiURLDefault, lokiURLUsage)
	flag.StringVar(&lokiURL, "l", lokiURLDefault, lokiURLUsage+" (shorthand)")
	flag.BoolVar(&settings.cleanup, "cleanup", cleanupDefault, cleanupUsage)
	flag.BoolVar(&settings.cleanup, "c", cleanupDefault, cleanupUsage+" (shorthand)")
	flag.IntVar(&from, "from", fromDefault, fromUsage)
	flag.IntVar(&from, "s", fromDefault, fromUsage)

	flag.Parse()

	if from > math.MaxUint8 {
		panic(fmt.Errorf("maximum from value is %d", math.MaxUint8))
	}

	settings.from = uint8(from)

	out, err := exec.Command("git", "branch", "--show-current").Output()
	if err != nil {
		panic(fmt.Errorf("%s: %w", string(out), err))
	}

	settings.branch = strings.ReplaceAll(strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(string(out)), "refs/"), "heads/"), "origin/"), ".", "-")

	orbCfg, err := orb.ParseOrbConfig(helpers.PruneHome(settings.orbconfig))
	if err != nil {
		panic(err)
	}

	if err := orb.IsComplete(orbCfg); err != nil {
		panic(err)
	}

	settings.orbID = strings.ToLower(strings.Split(strings.Split(orbCfg.URL, "/")[1], ".")[0])

	sendLevel := promtail.DISABLE
	if lokiURL != "" {
		fmt.Println("Sending logs to Loki")
		sendLevel = promtail.INFO
	}

	settings.logger, err = promtail.NewClientProto(promtail.ClientConfig{
		PushURL:            lokiURL,
		Labels:             fmt.Sprintf(`{e2e_test="true", branch="%s", orb="%s"}`, settings.branch, settings.orbID),
		BatchWait:          1 * time.Second,
		BatchEntriesNumber: 0,
		SendLevel:          sendLevel,
		PrintLevel:         promtail.INFO,
	})
	if err != nil {
		panic(err)
	}
	defer settings.logger.Shutdown()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	signalChannel := make(chan os.Signal)
	signal.Notify(signalChannel,
		syscall.SIGTERM,
		syscall.SIGINT,
		syscall.SIGQUIT,
	)

	go func() {
		<-signalChannel
		cancel()
	}()

	testFunc := run

	if graphiteURL != "" {
		fmt.Println("Sending status to Graphite")
		testFunc = graphite(
			graphiteURL,
			graphiteKey,
			run)
	}

	fmt.Println("Starting end-to-end test")
	fmt.Println(settings.String())

	if err := testFunc(ctx, settings); err != nil {
		settings.logger.Errorf("End-to-end test failed: %s", err.Error())
		returnCode = 1
	}
}
