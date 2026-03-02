---
handoff:
  id: feat-001
  from: user
  to: product-discovery
  created: 2026-02-23T00:00:00Z
  context_summary: "Build an end-to-end testing environment using test-infra repo with Flux, external-dns, DNS APIs, and eventually DNS Operator for complete testing from Gateway to authoritative DNS"
  decisions_made: []
  open_questions:
    - "What problem does this solve?"
    - "Who are the target users?"
    - "What are the critical user journeys?"
    - "What platform capabilities are needed?"
  assumptions: []
---

# Feature Request: E2E Test Environment with test-infra

**Requested by**: User
**Date**: 2026-02-23

## Initial Description

I want to build out an end-to-end testing environment built on the https://github.com/datum-cloud/test-infra/tree/main repo so we can end-to-end test this component and confirm all the components and tests work as expected.

The test environment should:
- Use Flux to install and configure external-dns
- Use Kustomize to deploy the webhook to the test infra cluster overlay
- Install the DNS APIs from Datum Cloud
- Plan for eventually installing the DNS Operator fully with a DNS provider like Knot DNS so we can truly get end-to-end functionality from provision Gateway to DIG auth DNS

## User Experience Goals

The goal is that a user can:
1. Run something like `dev:setup` and get a functional environment running with the webhook installed
2. Run `test:end-to-end` to execute the chainsaw-based end-to-end tests against the environment

## Context

- This repository already has Chainsaw-based E2E tests (see `e2e/` directory)
- Recent commits show work on E2E test infrastructure and replacing bash tests with Chainsaw
- The webhook integrates with external-dns and requires DNS APIs

## Notes

This request is awaiting discovery. The product-discovery agent will:
- Clarify the problem being solved
- Identify target users (developers, CI, etc.)
- Assess scope boundaries
- Evaluate platform capability requirements
- Produce a discovery brief
