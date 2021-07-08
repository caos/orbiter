package main

/*
import "github.com/caos/orbos/cmd/chore/e2e/shared"

func github(branch, token, testcase string, test func(orbconfig string) error) func(orbconfig string) error {

	send := func(status string) {
		if err := shared.Emit(shared.Event{
			EventType: "webhook-e2e-" + testcase,
			ClientPayload: map[string]string{
				"status": "running",
			},
			Branch: branch,
		}, token, "caos", "orbos"); err != nil {
			panic(err)
		}
	}

	return func(orbconfig string) error {
		send("running")

		result := "failure"
		err := test(orbconfig)
		if err == nil {
			result = "success"
		}
		send(result)
		return err
	}

}
*/
