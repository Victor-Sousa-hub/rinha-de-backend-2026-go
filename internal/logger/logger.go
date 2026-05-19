package logger

import (
	"log"
	"time"
)

const (
	reset   = "\033[0m"
	bold    = "\033[1m"
	dim     = "\033[2m"
	green   = "\033[32m"
	yellow  = "\033[33m"
	red     = "\033[31m"
	blue    = "\033[34m"
	magenta = "\033[35m"
	white   = "\033[37m"
)

func tag(color, label string) string {
	return bold + color + label + reset
}

func Startup(addr string, workers int) {
	log.Printf("%s  addr=%s  workers=%d", tag(magenta, "[startup]"), addr, workers)
}

func HTTP(method, path string, status, bytes int, elapsed time.Duration) {
	statusColor := green
	switch {
	case status >= 500:
		statusColor = red
	case status >= 400:
		statusColor = yellow
	}
	log.Printf("%s  %s %-20s  %s%d%s  %dB  %v",
		tag(dim+white, "[http   ]"),
		method, path,
		bold+statusColor, status, reset,
		bytes,
		elapsed.Round(time.Microsecond),
	)
}

func Ready(active, cap int) {
	log.Printf("%s  ativos=%d  cap=%d", tag(blue, "[ready  ]"), active, cap)
}
