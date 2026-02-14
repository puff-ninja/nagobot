---
name: skill-creator
description: Create or update agent skills. Use when designing, structuring, or packaging skills with references and assets.
---

# Skill Creator

This skill provides guidance for creating effective skills.

## About Skills

Skills are modular, self-contained packages that extend the agent's capabilities by providing specialized knowledge, workflows, and tools. They transform the agent from a general-purpose assistant into a specialized one equipped with procedural knowledge.

## Skill Structure

```
skill-name/
├── SKILL.md (required)
│   ├── YAML frontmatter (name, description)
│   └── Markdown instructions
└── Optional resources
    ├── scripts/       - Executable code
    ├── references/    - Documentation for context
    └── assets/        - Files used in output
```

## SKILL.md Frontmatter

Required fields:
- `name`: The skill name (lowercase, hyphens)
- `description`: What the skill does AND when to use it (this is the primary trigger)

Optional fields:
- `always: true` — Always include in agent context
- `metadata`: JSON with requirements check: `{"nagobot":{"requires":{"bins":["gh"],"env":["API_KEY"]}}}`

## Progressive Loading

Skills use a three-level loading system:
1. **Metadata (name + description)** — Always in system prompt (~100 words)
2. **SKILL.md body** — When skill triggers, agent reads via `read_file`
3. **Bundled resources** — As needed by the agent

## Creating a Skill

1. Create a directory in `workspace/skills/your-skill-name/`
2. Add a `SKILL.md` with YAML frontmatter and instructions
3. Optionally add `scripts/`, `references/`, `assets/` directories
4. The skill will be automatically discovered

## Guidelines

- Keep SKILL.md concise — the agent is smart, only add non-obvious knowledge
- Put detailed reference material in `references/` files, not in SKILL.md
- Write descriptions that clearly indicate when the skill should be triggered
- Use imperative form in instructions
