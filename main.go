package main

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"qms/internal/api"
	"qms/internal/controller"
	"qms/internal/order"
)

func main() {
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "--demo" {
		runDemo(os.Stdout)
		return
	}
	if len(args) > 0 && args[0] == "--server" {
		addr := ":8080"
		if len(args) > 1 {
			addr = args[1]
		}
		runServer(addr)
		return
	}
	runInteractive(os.Stdin, os.Stdout)
}

func runServer(addr string) {
	dur := processDuration()
	c := controller.New(
		controller.WithProcessDuration(dur),
		controller.WithLogger(controller.NewWriterLogger(os.Stdout)),
	)
	fmt.Printf("QMS HTTP server listening on %s\n", addr)
	fmt.Printf("  POST   /orders   {\"type\":\"normal\"|\"vip\"}\n")
	fmt.Printf("  POST   /bots\n")
	fmt.Printf("  DELETE /bots\n")
	fmt.Printf("  GET    /state         (HTTP JSON snapshot or WebSocket stream)\n")
	if err := http.ListenAndServe(addr, api.New(c)); err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}

// runDemo executes a scripted simulation that exercises every requirement and
// writes timestamped output to w.
func runDemo(w io.Writer) {
	dur := processDuration()
	log := controller.NewWriterLogger(w)
	c := controller.New(
		controller.WithProcessDuration(dur),
		controller.WithLogger(log),
	)

	printHeader(w, dur)

	// Req 4 & 5: add bots before orders to show idle state
	section(w, "Step 1 – Add 2 bots (no orders yet; bots start IDLE)")
	c.AddBot()
	c.AddBot()
	sleep(w, 200*time.Millisecond, "bots settle idle")
	printState(w, c)

	// Req 1 & 3: normal orders with unique IDs
	section(w, "Step 2 – Add 3 Normal orders")
	c.NewOrder(order.Normal) // #1
	c.NewOrder(order.Normal) // #2
	c.NewOrder(order.Normal) // #3
	sleep(w, 100*time.Millisecond, "bots pick up work")
	printState(w, c)

	// Req 2: VIP order jumps queue
	section(w, "Step 3 – Add 1 VIP order (should jump ahead of Normal #3)")
	c.NewOrder(order.VIP) // #4
	sleep(w, 50*time.Millisecond, "queue settles")
	printState(w, c)

	// Let first batch complete
	section(w, "Step 4 – Wait for first orders to complete")
	time.Sleep(dur + dur/2)
	printState(w, c)

	// Req 6: remove bot – its order returns to PENDING
	section(w, "Step 5 – Add 2 more orders, then remove a bot mid-processing")
	c.NewOrder(order.Normal) // #5
	c.NewOrder(order.VIP)    // #6 – VIP should jump ahead of #5
	sleep(w, 50*time.Millisecond, "queue settles")
	printState(w, c)

	fmt.Fprintf(w, "\n")
	c.RemoveBot()
	sleep(w, 100*time.Millisecond, "bot goroutine returns its order")
	printState(w, c)

	// Req 4 again: new bot picks up remaining work
	section(w, "Step 6 – Add a new bot to drain remaining queue")
	c.AddBot()
	time.Sleep(dur*3 + dur/2)
	printState(w, c)

	// Final summary
	section(w, "Final State")
	printState(w, c)
	fmt.Fprintf(w, "\nAll requirements demonstrated.\n")
}

func printHeader(w io.Writer, dur time.Duration) {
	border := strings.Repeat("=", 60)
	fmt.Fprintf(w, "%s\n", border)
	fmt.Fprintf(w, "  Queue Management System (QMS) – Automated Order Management System\n")
	fmt.Fprintf(w, "%s\n", border)
	fmt.Fprintf(w, "Processing time per order: %v\n\n", dur)
}

func section(w io.Writer, title string) {
	ts := time.Now().Format("15:04:05")
	fmt.Fprintf(w, "\n[%s] ── %s ──\n", ts, title)
}

func sleep(w io.Writer, d time.Duration, _ string) {
	time.Sleep(d)
}

func printState(w io.Writer, c *controller.Controller) {
	state := c.State()
	ts := time.Now().Format("15:04:05")

	fmt.Fprintf(w, "[%s] PENDING (%d):", ts, len(state.Pending))
	if len(state.Pending) == 0 {
		fmt.Fprintf(w, " (empty)")
	}
	for _, o := range state.Pending {
		fmt.Fprintf(w, " [#%d %s]", o.ID, o.Type)
	}
	fmt.Fprintln(w)

	fmt.Fprintf(w, "[%s] BOTS    (%d):", ts, len(state.Bots))
	for _, b := range state.Bots {
		if b.CurrentOrder != nil {
			fmt.Fprintf(w, " [Bot#%d→Order#%d]", b.ID, b.CurrentOrder.ID)
		} else {
			fmt.Fprintf(w, " [Bot#%d→IDLE]", b.ID)
		}
	}
	fmt.Fprintln(w)

	fmt.Fprintf(w, "[%s] COMPLETE(%d):", ts, len(state.Completed))
	for _, o := range state.Completed {
		fmt.Fprintf(w, " [#%d %s]", o.ID, o.Type)
	}
	fmt.Fprintln(w)
}

// processDuration reads PROCESS_SECONDS from the environment (default 10).
// The demo script sets it to a lower value so CI finishes quickly.
func processDuration() time.Duration {
	if s := os.Getenv("PROCESS_SECONDS"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	return 10 * time.Second
}

// runInteractive starts a read-eval loop on r, writing output to w.
func runInteractive(r io.Reader, w io.Writer) {
	dur := processDuration()
	log := controller.NewWriterLogger(w)
	c := controller.New(
		controller.WithProcessDuration(dur),
		controller.WithLogger(log),
	)

	fmt.Fprintf(w, "Queue Management System (QMS) – Interactive Order Management System\n")
	fmt.Fprintf(w, "Processing time: %v per order\n", dur)
	fmt.Fprintf(w, "Commands: new normal | new vip | +bot | -bot | status | help | quit\n\n")

	scanner := bufio.NewScanner(r)
	for {
		fmt.Fprintf(w, "> ")
		if !scanner.Scan() {
			break
		}
		cmd := strings.TrimSpace(strings.ToLower(scanner.Text()))
		if cmd == "" {
			continue
		}

		switch cmd {
		case "new normal", "n", "normal":
			c.NewOrder(order.Normal)

		case "new vip", "v", "vip":
			c.NewOrder(order.VIP)

		case "+bot", "bot+", "+", "add":
			c.AddBot()

		case "-bot", "bot-", "-", "remove":
			c.RemoveBot()

		case "status", "s":
			printState(w, c)

		case "help", "h", "?":
			printHelp(w)

		case "quit", "exit", "q":
			fmt.Fprintf(w, "Goodbye.\n")
			return

		default:
			fmt.Fprintf(w, "Unknown command: %q (type 'help' for usage)\n", cmd)
		}
	}
}

func printHelp(w io.Writer) {
	fmt.Fprintf(w, `
Commands
  new normal  (n)       Add a Normal order to the queue
  new vip     (v)       Add a VIP order (jumps ahead of all Normal orders)
  +bot        (+)       Create a new cooking bot
  -bot        (-)       Destroy the newest bot (order returns to PENDING)
  status      (s)       Show current PENDING / BOTS / COMPLETE state
  quit        (q)       Exit
`)
}
