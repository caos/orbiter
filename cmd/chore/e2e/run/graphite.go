package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"gopkg.in/raintank/schema.v1"
)

type runFunc func(context.Context, programSettings) error

func graphite(cloudURL, cloudKey string, test runFunc) runFunc {

	return func(ctx context.Context, settings programSettings) error {
		send := func(value float64, ts time.Time) {
			if err := sendGraphiteStatus(settings.orbID, cloudURL, cloudKey, settings.branch, value, ts); err != nil {
				panic(err)
			}
		}

		start := time.Now()
		send(0.5, start)
		err := test(ctx, settings)
		var value float64 = 0
		if err == nil {
			value = 1
		}
		stop := time.Now()
		minStop := start.Add(2 * time.Minute)
		if minStop.After(stop) {
			stop = minStop
		}
		send(value, stop)
		return err
	}
}

func sendGraphiteStatus(orbID, cloudURL, cloudKey, branch string, value float64, ts time.Time) error {

	name := fmt.Sprintf("e2e.%s.%s", orbID, branch)

	metrics := schema.MetricDataArray{&schema.MetricData{
		Name:     name,
		Interval: 10,
		Value:    value,
		Time:     ts.Unix(),
		Mtype:    "gauge",
	}}

	// encode as json
	data, err := json.Marshal(metrics)
	if err != nil {
		return err
	}

	client := &http.Client{}

	req, err := http.NewRequest("POST", cloudURL, bytes.NewBuffer(data))
	if err != nil {
		return err
	}

	req.Header.Add("Authorization", "Bearer "+cloudKey)
	req.Header.Add("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	buf := make([]byte, 4096)
	n, err := resp.Body.Read(buf)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("sending metric to graphana cloud graphite api at %s failed with status %s and response %s", cloudURL, resp.Status, string(buf[:n]))
	}
	fmt.Println("Metric", name, "with value", value, "sent to grafana cloud graphite api")
	return nil
}
