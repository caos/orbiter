package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

func main() {

	var (
		token, org, repository, branch string
		from                           int
		cleanup                        bool
	)

	flag.StringVar(&token, "access-token", "", "Personal access token with repo scope")
	flag.StringVar(&org, "organization", "", "Github organization")
	flag.StringVar(&repository, "repository", "", "Github project")
	flag.StringVar(&branch, "branch", "", "Branch to test. Default is current")
	flag.IntVar(&from, "from", 1, "From e2e test stage")
	flag.BoolVar(&cleanup, "cleanup", true, "Cleanup after tests are done")

	flag.Parse()

	if branch == "" {
		ref, err := exec.Command("git", "branch", "--show-current").Output()
		if err != nil {
			panic(err)
		}
		branch = strings.TrimPrefix(strings.TrimSpace(string(ref)), "heads/")
	}

	fmt.Printf("organization=%s\n", org)
	fmt.Printf("repository=%s\n", repository)
	fmt.Printf("branch=%s\n", branch)
	fmt.Printf("from=%d\n", from)
	fmt.Printf("cleanup=%t\n", cleanup)

	if err := emit(event{
		EventType: "webhook-trigger",
		ClientPayload: map[string]string{
			"branch":  branch,
			"from":    strconv.Itoa(from),
			"cleanup": strconv.FormatBool(cleanup),
		},
	}, token, org, repository); err != nil {
		panic(err)
	}
}

type event struct {
	EventType     string            `json:"event_type,omitempty"`
	ClientPayload map[string]string `json:"client_payload,omitempty"`
	Branch        string            `json:"branch,omitempty"`
}

func emit(event event, accessToken, organisation, repository string) error {

	data, err := json.Marshal(event)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/dispatches", organisation, repository)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return err
	}

	req.Header.Set("Accept", "application/vnd.github.everest-preview+json")
	req.Header.Set("Authorization", fmt.Sprintf("token %s", accessToken))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	_, err = io.Copy(os.Stdout, resp.Body)
	if err != nil {
		return err
	}
	fmt.Println(resp.Status)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("emitting github repository dispatch event at %s failed", url)
	}
	return nil
}
