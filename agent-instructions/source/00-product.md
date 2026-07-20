# Agent Orchestrator

Agent Orchestrator is a Go daemon and Electron supervisor for coordinating
coding-agent sessions. The backend owns durable lifecycle state, storage,
runtime adapters, and the loopback HTTP API. The frontend is a thin supervisor
surface over the generated API client.

This fork tracks upstream closely while carrying Polymath SDLC wiring and a
curated set of ported features. Keep product changes small, rebase-friendly,
and suitable for later upstream submission unless a ticket is explicitly
fork-only.

