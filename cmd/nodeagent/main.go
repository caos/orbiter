package main

import (
	"context"
	"crypto/sha256"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/caos/orbos/internal/git"
	"github.com/caos/orbos/mntr"

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

	flag.Parse()

	if *printVersion {
		fmt.Printf("%s %s\n", version, gitCommit)
		os.Exit(0)
	}

	if *nodeAgentID == "" {
		panic("flags --repourl and --id are required")
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
	conv := conv.New(monitor, os, fmt.Sprintf("%x", hashed[:]))

	gitClient := git.New(context.Background(), monitor, fmt.Sprintf("Node Agent %s", *nodeAgentID))

	var portsSlice []string
	if len(*ignorePorts) > 0 {
		portsSlice = strings.Split(*ignorePorts, ",")
	}

	itFunc := nodeagent.Iterator(
		monitor,
		gitClient,
		gitCommit,
		*nodeAgentID,
		firewall.Ensurer(monitor, os.OperatingSystem, portsSlice),
		conv,
		conv.Init())

	for {
		itFunc()
		monitor.Info("Iteration done")
		time.Sleep(10 * time.Second)
	}
}
