package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"ai_oauth_proxy/metrics"
	"ai_oauth_proxy/server"
	"ai_oauth_proxy/tui"
)

func main() {
	// Parse command line options
	modeFlag := flag.String("mode", "chat", "Mode to run the tool: 'server' or 'chat'")
	portFlag := flag.Int("port", 8080, "Port to run the HTTP proxy server on (only used in server mode)")
	flag.Parse()

	// Handle subcommands or positional arguments to make invocation easy
	mode := *modeFlag
	if flag.NArg() > 0 {
		arg0 := flag.Arg(0)
		if arg0 == "server" || arg0 == "proxy" {
			mode = "server"
		} else if arg0 == "chat" || arg0 == "tui" || arg0 == "cli" {
			mode = "chat"
		}
	}

	// Initialize thread-safe metrics tracker
	tracker := metrics.NewTokenTracker()

	switch mode {
	case "server":
		// Handle graceful termination to print token summary on exit
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

		go func() {
			<-sigChan
			log.Println("\nShutdown signal received. Exiting gracefully...")
			tracker.PrintSummary()
			os.Exit(0)
		}()

		srv := server.NewServer(*portFlag, tracker)
		if err := srv.Start(); err != nil {
			log.Fatalf("Proxy server failed: %v", err)
		}

	case "chat":
		// Launch terminal Chat TUI
		if err := tui.StartTUI(tracker); err != nil {
			fmt.Printf("Error running Chat TUI: %v\n", err)
			os.Exit(1)
		}

		// Print beautiful token usage and daily estimation summary on exit
		tracker.PrintSummary()

	default:
		fmt.Printf("Unknown mode: %s. Supported modes are 'server' and 'chat'.\n", mode)
		os.Exit(1)
	}
}
