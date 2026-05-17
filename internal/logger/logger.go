package logger

import (
	"log"
	"time"
)

const (
	reset   = "\033[0m"
	bold    = "\033[1m"
	dim     = "\033[2m"
	cyan    = "\033[36m"
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

func Startup(addr string, workers, queueCap int) {
	log.Printf("%s  addr=%s  workers=%d  queue_cap=%d", tag(magenta, "[startup]"), addr, workers, queueCap)
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

func Enqueue(txID string, queueLen int) {
	log.Printf("%s  tx=%-20s  fila=%d", tag(cyan, "[enqueue]"), txID, queueLen)
}

func WorkerIn(txID string, active int64, queueLen int) {
	log.Printf("%s  tx=%-20s  ativos=%d  fila=%d", tag(yellow, "[worker+]"), txID, active, queueLen)
}

func WorkerOut(txID string, active int64, queueLen int) {
	log.Printf("%s  tx=%-20s  ativos=%d  fila=%d", tag(green, "[worker-]"), txID, active, queueLen)
}

func Reject(txID string, cap int) {
	log.Printf("%s  tx=%-20s  cap=%d", tag(red, "[reject ]"), txID, cap)
}

func Ready(queueLen, active, cap int) {
	log.Printf("%s  fila=%d  ativos=%d  cap=%d", tag(blue, "[ready  ]"), queueLen, active, cap)
}
