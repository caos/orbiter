package main

import (
	"context"
	"crypto/sha256"
	"flag"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/caos/orbos/internal/operator/nodeagent/networking"

	"github.com/caos/orbos/mntr"
	"github.com/caos/orbos/pkg/git"

	_ "net/http/pprof"

	"github.com/caos/orbos/internal/operator/nodeagent"
	"github.com/caos/orbos/internal/operator/nodeagent/dep"
	"github.com/caos/orbos/internal/operator/nodeagent/dep/conv"
	"github.com/caos/orbos/internal/operator/nodeagent/firewall"
)

var gitCommit string
var version string

func main() {

	defer func() {
		r := recover()
		if r != nil {
			os.Stderr.Write([]byte(fmt.Sprintf("\x1b[0;31m%v\x1b[0m\n", r)))
			os.Exit(1)
		}
	}()

	verbose := flag.Bool("verbose", false, "Print logs for debugging")
	printVersion := flag.Bool("version", false, "Print build information")
	ignorePorts := flag.String("ignore-ports", "", "Comma separated list of firewall ports that are ignored")
	nodeAgentID := flag.String("id", "", "The managed machines ID")
	pprof := flag.Bool("pprof", false, "start pprof as port 6060")

	flag.Parse()

	if *printVersion {
		fmt.Printf("%s %s\n", version, gitCommit)
		os.Exit(0)
	}

	if *nodeAgentID == "" {
		panic("flag --id is required")
	}
	monitor := mntr.Monitor{
		OnInfo:   mntr.LogMessage,
		OnChange: mntr.LogMessage,
		OnError:  mntr.LogError,
	}

	if *verbose {
		monitor = monitor.Verbose()
	}

	monitor.WithFields(map[string]interface{}{
		"version":     version,
		"commit":      gitCommit,
		"verbose":     *verbose,
		"nodeAgentID": *nodeAgentID,
	}).Info("Node Agent is starting")

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

	if *pprof {
		go func() {
			monitor.Info(http.ListenAndServe("localhost:6060", nil).Error())
		}()
	}

	os, err := dep.GetOperatingSystem()
	if err != nil {
		panic(err)
	}

	repoKey, err := nodeagent.RepoKey()
	if err != nil {
		panic(err)
	}

	pruned := strings.Split(string(repoKey), "-----")[2]
	hashed := sha256.Sum256([]byte(pruned))
	conv := conv.New(ctx, monitor, os, fmt.Sprintf("%x", hashed[:]))

	gitClient := git.New(ctx, monitor, fmt.Sprintf("Node Agent %s", *nodeAgentID), "node-agent@caos.ch")

	var portsSlice []string
	if len(*ignorePorts) > 0 {
		portsSlice = strings.Split(*ignorePorts, ",")
	}

	itFunc := nodeagent.Iterator(
		ctx,
		monitor,
		gitClient,
		gitCommit,
		*nodeAgentID,
		firewall.Ensurer(ctx, monitor, os.OperatingSystem, portsSlice),
		networking.Ensurer(ctx, monitor, os.OperatingSystem),
		conv,
		conv.Init())

	daily := time.NewTicker(24 * time.Hour)
	defer daily.Stop()
	update := make(chan struct{})
	go func() {
		for range daily.C {
			timer := time.NewTimer(time.Duration(rand.Intn(120)) * time.Minute)
			<-timer.C
			update <- struct{}{}
			timer.Stop()
		}
	}()

	iterate := make(chan struct{})
	//trigger first iteration
	go func() { iterate <- struct{}{} }()
	for {
		select {
		case <-iterate:
			monitor.Info("Starting iteration")
			itFunc()
			monitor.Info("Iteration done")
			time.Sleep(10 * time.Second)
			//trigger next iteration
			go func() { iterate <- struct{}{} }()
		case <-update:
			monitor.Info("Starting update")
			if err := conv.Update(); err != nil {
				monitor.Error(fmt.Errorf("updating packages failed: %w", err))
			} else {
				monitor.Info("Update done")
			}
		}
	}
}
