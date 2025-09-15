---
# Banner
banner:
  title: "Forq: a simple Message Queue powered by SQLite"
  content: "The missing middle between embedded libraries and enterprise solutions. Great for small to medium workloads of up to a few hundred messages per second."
  image: "/images/banner-light.png"
  button:
    enable: true
    label: "View Documentation"
    link: "/docs/"

# Features
features:
  - title: "Simple & Reliable"
    image: "/images/db.png"
    content: "No PhD required: a queue that just works without complexity."
    bulletpoints:
      - "**Single binary or Docker:** no external dependencies required to run"
      - "**Built with Boring stack:** Go + SQLite + HTMX"
      - "**1 table schema, 4 endpoints API:** easy to understand and use"
      - "**Reasonable defaults:** just pass the auth secret and go"
      - "**Admin UI included:** for monitoring and management"
      - "**Prometheus metrics:** for observability and alerting"
    button:
      enable: true
      label: "Get Started"
      link: "/docs/guides/getting-started/"

  - title: "Production Ready Features"
    image: "/images/benchmarks.png"
    content: "Designed for real workloads with enterprise-grade reliability in a simple package."
    bulletpoints:
      - "**Decent throughput:** 1200+ messages per sec on my old MacBook"
      - "**Retry logic** with exponential backoff and dead letter queues"
      - "**Message TTL** with automatic cleanup of expired messages"
      - "**Delayed messages** for scheduling future work"
      - "**At-least-once delivery**"
      - "**FIFO order**"
    button:
      enable: true
      label: "View API Docs"
      link: "/docs/reference/api/"

  - title: "Developer Experience First"
    image: "/images/apidoc.png"
    content: "Built for developers who value simplicity, reliability, and their own time."
    bulletpoints:
      - "**Language agnostic:** works with any HTTP client"
      - "**Official SDKs**: Go, Java, TypeScript"
      - "**Environment-only config:** no passwords in the shell history"
      - "**Lightweight** with a small memory and CPU footprint"
      - "**Clear error messages** and comprehensive logging"
      - "**Easy to understand and debug internals**"
    button:
      enable: true
      label: "View OpenAPI Spec"
      link: "https://github.com/n0rdy/forq/blob/main/openapi.yaml"

  - title: "But Very Opinionated"
    image: "/images/contributing.png"
    content: "Not a swiss-army knife. Focused on doing one thing well, with minimal configuration."
    bulletpoints:
      - "**Non-configurable defaults:** sensible for most use-cases"
      - "**No rate limiting:** that's on you"
      - "**No DB backups:** it's just SQLite files, run Linux cronjob and send them to S3 or smth"
      - "**One auth secret to rule them all:** for both consumers, producers, and the admin UI"
      - "**Open-source, but closed-contribution:** no PRs, please"
    button:
      enable: true
      label: "More Opinions"
      link: "/docs/guides/opinions/"
---
