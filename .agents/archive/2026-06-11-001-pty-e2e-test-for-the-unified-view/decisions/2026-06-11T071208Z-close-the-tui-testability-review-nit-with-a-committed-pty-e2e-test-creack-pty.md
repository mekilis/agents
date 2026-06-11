# Decision: Close the TUI-testability review nit with a committed PTY e2e test (creack/pty +

Status: proposed
By: smart

Close the TUI-testability review nit with a committed PTY e2e test (creack/pty + test-binary helper-process pattern) instead of leaving terminal coverage manual: the test runs agents go on a real pseudo-terminal and asserts split view, ready screen, and accept/archive in CI; creack/pty is test-only
