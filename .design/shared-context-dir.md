## Feature request: shared context dir

have a grove level shared context directory that agents can use to share information and status between themselves.

'./scion/shared_context'

mounted in container to /shared_context

passed to the agent with --include-directories

potentially have a hook that periodically looks for additions to this, and reviews context for relvance and injects it into agent context

postbox idea:

.agent_communication/
├── registry.json       # Lists active agents and their capabilities
├── broadcast.log       # Shared "Channel" for all agents
└── inbox/
    ├── claude_1.json   # DM folder for Claude
    └── gemini_1.json   # DM folder for Gemini