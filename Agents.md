# Agent Instructions

This is a production performance-sensitive Go project.

Never invent product facts.

Product source data is authoritative.

LLM-generated descriptions must only reformulate
information present in source data.

Always preserve exact EAN values.

Never modify source data.

Run validation after modifying code.

Prefer existing project architecture over creating duplicate systems.

Do not hardcode API credentials.

Do not expose secrets in logs.

For mass processing, skip already completed valid records.

When processing large datasets, continue after individual item errors.
