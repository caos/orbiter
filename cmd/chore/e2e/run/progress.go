package main

import (
	"math"
	"strings"
	"time"
)

func printProgress(prefix operatorPrefix, settings programSettings, step string, started time.Time, timeout time.Duration) {
	elapsed := int(math.Round(float64(time.Now().Sub(started)) / float64(timeout) * 100))
	left := 100 - elapsed

	logProgress := settings.logger.Infof
	if elapsed > 85 {
		logProgress = settings.logger.Warnf
	}
	logProgress("%s%s step %s timeout status %s [%s%s] %s (%d%%)\n",
		prefix,
		settings.orbID,
		step,
		started.Format("15:04:05"),
		strings.Repeat("#", elapsed),
		strings.Repeat("_", left),
		started.Add(timeout).Format("15:04:05"), elapsed)
}
