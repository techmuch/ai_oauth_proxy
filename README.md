# AI OAuth Proxy & TUI Tool

A premium, custom proxy and Text-Based User Interface (TUI) written in Go. The tool acts as a protocol translator and subprocess manager, exposing OpenAI-compatible REST endpoints while executing the localized `claude` CLI autonomously on the host machine. 

It features thread-safe token tracking and cost projection middleware, dual-mode execution (HTTP Server / Bubble Tea TUI), automatic superuser `systemd` background daemonization, and a robust process-group termination lifecycle engine to guarantee no orphan processes.

---

## Technical Architecture

```mermaid
graph TD
    subgraph Client Apps
        Harness[Hermes/OpenAI Client]
    end
    subgraph Proxy App (Go Binary)
        Server[HTTP Server Mode]
        TUI[Chat/TUI Mode]
        Engine[Translation & Subprocess Manager]
        Tracker[Token Tracker / Session Metrics]
    end
    subgraph Local Environment
        Claude[Claude Code CLI /root/.local/bin/claude]
    end

    Harness -->|OpenAI JSON Request| Server
    Server -->|Parse & Flatten| Engine
    TUI -->|User Input| Engine
    Engine -->|Pipe stdin| Claude
    Claude -->|stream-json stdout| Engine
    Engine -->|Parse stream-json| Tracker
    Engine -->|Translate SSE| Server
    Engine -->|Render UI| TUI
    Server -->|OpenAI SSE Stream| Harness
```

---

## Key Features

- **OpenAI-Compatible Server Mode**: Exposes `/v1/chat/completions` and `/v1/models` endpoints to drop directly into standard automated harnesses (such as Hermes).
- **Interactive Bubble Tea TUI Mode**: A gorgeous, dark-themed terminal UI designed with Lip Gloss borders, live-updating statistics sidebars, scrolling viewports, and interactive textareas.
- **Zero-Token Auth Security Model**: Never stores, logs, or handles live authentication keys. Automatically strips Bearer tokens from incoming HTTP requests, relying exclusively on the pre-authenticated local state of the `claude` CLI on the host machine.
- **SSE Streaming Engine**: Translates stdout streaming events from the CLI process into high-fidelity OpenAI SSE chunks in real time.
- **Thread-Safe Metrics**: Dynamically logs sent/received tokens and calculates billing estimates based on active Anthropic Sonnet rates, printing a clean summary table upon exit.
- **Orphan Prevention**: Spawns CLI subprocesses inside an isolated Unix process group, guaranteeing all descendant processes are terminated with a group SIGKILL signal if a client connection closes mid-stream.
- **Automated systemd Installer**: A self-contained `install.sh` script to compile the binary, move it to `/usr/local/bin`, register, enable, and start a managed background systemd daemon running under the pre-authenticated `root` user context.

---

## Project Structure

```
├── main.go               # Entrypoint (argument parser & system signal listeners)
├── install.sh            # Automated superuser systemd installer script
├── .gitignore            # Excludes compiled binary and test coverage assets
├── README.md             # Complete project documentation
├── engine/
│   ├── runner.go         # Spawns CLI, pipes stdin, parses stdout stream-json
│   └── runner_test.go    # Unit tests for transcript flattener and JSON line parser
├── metrics/
│   ├── tracker.go        # Thread-safe counter, cost calculators, exit summarizer
│   └── tracker_test.go   # Unit tests for cost calculations and aggregator values
├── server/
│   ├── server.go         # HTTP router, OpenAI response builders, SSE, CORS, logging
│   └── server_test.go    # Unit tests for models responses and CORS middleware headers
└── tui/
    └── tui.go            # Bubble Tea models, Viewports, Textareas, Lip Gloss UI
```

---

## Installation & Deployment

Deploying the proxy as a managed `systemd` background service is completely automated:

1. **Verify Prerequisites**:
   Ensure Go (1.24+) is installed and that the `claude` CLI (`/root/.local/bin/claude`) is pre-authenticated.

2. **Execute Deployment Script**:
   Run the installer script under root/sudo privileges:
   ```bash
   sudo ./install.sh
   ```

   This script will automatically:
   - Check superuser privileges.
   - Build the Go binary if not already compiled.
   - Install the executable binary to `/usr/local/bin/ai_oauth_proxy`.
   - Setup executable permissions (`755`).
   - Create and load the `/etc/systemd/system/ai-oauth-proxy.service` file.
   - Enable and start the service instantly at system boot.
   - Validate port responsiveness on port `8080` via a local loopback curl check.

---

## Usage Guide

The single binary supports two distinct modes:

### 1. Server Mode (Background Daemon)
Runs an OpenAI-compatible API listener. The `systemd` service manages this by default:
- **Service commands**:
  ```bash
  sudo systemctl start ai-oauth-proxy.service     # Start server
  sudo systemctl stop ai-oauth-proxy.service      # Stop server (prints token summary to journal)
  sudo systemctl status ai-oauth-proxy.service    # Check server health
  journalctl -u ai-oauth-proxy.service -f         # Stream live traffic logs
  ```

- **Querying the Endpoint**:
  ```bash
  curl -i -X POST http://localhost:8080/v1/chat/completions \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer dummy_token" \
    -d '{
      "model": "claude-sonnet-4-6",
      "messages": [{"role": "user", "content": "Write a 3-word response about data."}],
      "stream": false
    }'
  ```

### 2. Chat / TUI Mode (Interactive Terminal)
Launches the full interactive terminal chat UI:
- **Run the TUI**:
  ```bash
  ./ai_oauth_proxy chat
  ```

- **TUI Keybindings**:
  - `Ctrl + C`: Interrupts streaming or exits cleanly (if draft is empty).
  - `Ctrl + L`: Forces a full terminal redraw (preserving viewport history).
  - `Ctrl + R`: Reverse-search prompt command history.
  - `Esc`: Interrupts current response streaming mid-turn to redirect conversation.

---

## Testing

Run the automated test suite to verify module mechanics:
```bash
go test -v ./...
```
Output:
```
?       ai_oauth_proxy          [no test files]
=== RUN   TestFlattenMessages
--- PASS: TestFlattenMessages (0.00s)
=== RUN   TestStreamLineParsing
--- PASS: TestStreamLineParsing (0.00s)
PASS
ok      ai_oauth_proxy/engine   0.002s
?       ai_oauth_proxy/metrics  [no test files]
=== RUN   TestTokenTracker
--- PASS: TestTokenTracker (0.00s)
PASS
ok      ai_oauth_proxy/metrics  0.002s
=== RUN   TestHandleModels
--- PASS: TestHandleModels (0.00s)
=== RUN   TestCORSHeaders
--- PASS: TestCORSHeaders (0.00s)
PASS
ok      ai_oauth_proxy/server   0.003s
?       ai_oauth_proxy/tui      [no test files]
```
