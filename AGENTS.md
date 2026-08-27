# Project Overview

Read `README.md` to understand:
- The purpose of this project
- The project vision and goals
- The architectural principles
- The data model philosophy

The README is the canonical source for project context. This document only contains instructions for AI agents.

# Guidelines

* When creating or modifying unit tests, add comments only for non-obvious setup that is essential to the scenario, such as simulating a concurrent worker state, deliberately forcing a dependency failure, or supplying an unsupported value to verify rejection before a side effect.
* Place the comment immediately above the meaningful setup line and explain why the setup exists. Do not restate what the code already says.
* Do not comment routine test construction, mock configuration, method calls, or assertions when the test name and code make their purpose clear.
* Use UTC for timestamps created or persisted by application code.

# Restricted Operations

* Never commit to the repository.
* Never push to the repository.
* Never create pull requests unless explicitly requested.
* Never make external cloud, production, S3, or third-party service changes without explicit approval.
* Never scan the `data`, `.uv-cache`, `node_modules`, `.terraform` or any vendor folder.
* Do not directly read the `.env*` file.
* Do not read secrets files, credentials, private keys, or local config containing tokens unless explicitly approved.
* Do not revert or overwrite user changes unless explicitly asked.
* Do not modify pre-existing app data except for read-only inspection unless explicitly requested and approved.
* Create only uniquely named test data and clean it up when verification is complete.
* Never make manual infrastructure changes, infrastructure changes should keep to infrastructure as code as much as possible and require approval from the user to bypass.
