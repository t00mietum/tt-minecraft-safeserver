<!-- markdownlint-disable MD007 -- Unordered list indentation -->
<!-- markdownlint-disable MD010 -- No hard tabs -->
<!-- markdownlint-disable MD033 -- No inline html -->
<!-- markdownlint-disable MD055 -- Table pipe style [Expected: leading_and_trailing; Actual: leading_only; Missing trailing pipe] -->
<!-- markdownlint-disable MD041 -- First line in a file should be a top-level heading -->
<div align="center">

[![License: GPL v2+](https://img.shields.io/badge/License-GPLv2%2B-blue.svg)](https://www.gnu.org/licenses/old-licenses/gpl-2.0.html)
![Lifecycle: Alpha](https://img.shields.io/badge/Lifecycle-Alpha-orange)
![Support](https://img.shields.io/badge/Support-Maintained-brightgreen)
![Status: Passing](https://img.shields.io/badge/Status-Passing-brightgreen)

</div>
<!--
[![!#/bin/bash](https://img.shields.io/badge/-%23!%2Fbin%2Fbash-1f425f.svg?logo=gnu-bash)](https://www.gnu.org/software/bash/)
[![made-with-python](https://img.shields.io/badge/Made%20with-Python-1f425f.svg)](https://www.python.org/)
[![made-with-rust](https://img.shields.io/badge/Made%20with-Rust-1f425f.svg)](https://www.rust-lang.org/)
![Go](https://img.shields.io/badge/Go-00ADD8?logo=go&logoColor=white)
![Made with](https://img.shields.io/badge/Made%20with-C%2B%2B-brightgreen?style=plastic)
![Made with](https://img.shields.io/badge/Made%20with-Unreal%20Engine-critical?style=plastic)
[![made-with-javascript](https://img.shields.io/badge/Made%20with-JavaScript-1f425f.svg)](https://www.javascript.com)
![License: GPL v2](https://img.shields.io/badge/License-GPLv2-blue.svg)
[![License: GPL v2+](https://img.shields.io/badge/License-GPLv2%2B-blue.svg)](https://www.gnu.org/licenses/old-licenses/gpl-2.0.html)
![License: GPL v3](https://img.shields.io/badge/License-GPLv3-blue.svg)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![License: MPL 2.0](https://img.shields.io/badge/License-MPL_2.0-brightgreen.svg)](https://opensource.org/licenses/MPL-2.0)
![Lifecycle: Alpha](https://img.shields.io/badge/Lifecycle-Alpha-orange)
![Lifecycle: Beta](https://img.shields.io/badge/Lifecycle-Beta-yellow)
![Lifecycle: RC](https://img.shields.io/badge/Lifecycle-RC-blue)
![Lifecycle: Stable](https://img.shields.io/badge/Lifecycle-Stable-brightgreen)
![Lifecycle: Deprecated](https://img.shields.io/badge/Lifecycle-Deprecated-red)
![Status: Deprecated](https://img.shields.io/badge/Status-Deprecated-orange)
![Status: Archived](https://img.shields.io/badge/Status-Archived-lightgrey)
![Lifecycle: EOL](https://img.shields.io/badge/Lifecycle-EOL-lightgrey)
![Coverage](https://img.shields.io/badge/Coverage-25%25-red)
![Coverage](https://img.shields.io/badge/Coverage-50%25-orange)
![Coverage](https://img.shields.io/badge/Coverage-75%25-yellow)
![Coverage](https://img.shields.io/badge/Coverage-90%25-brightgreen)
![Status: Passing](https://img.shields.io/badge/Status-Passing-brightgreen)
![Status: Failing](https://img.shields.io/badge/Status-Failing-red)
[![GitHub Sponsors](https://img.shields.io/github/sponsors/t00mietum?logo=GitHub%20Sponsors&style=social)](https://github.com/sponsors/t00mietum)
-->

<!-- TOC ignore:true -->
# tt-minecraft-safeserver

<!--
<table style="border: none; border-collapse: collapse;">
	<tr style="border: none; border-collapse: collapse;">
		<td style="border: none; border-collapse: collapse;"><img src="assets/logo.png" alt="Logo" width="320"/></td>
		<td style="border: none;">tt-minecraft-safeserver runs a Paper Minecraft server for friends and family under its own unprivileged account, in a locked-down container. Its tool, `tt-minecraftd`, starts, stops, sets up, and updates the server, and puts the safe settings back in place before every start.</td>
	</tr>
</table>

<p align="center"><img src="assets/logo.png" alt="" width="128"></p>
-->

tt-minecraft-safeserver runs a Paper Minecraft server for friends and family under its own unprivileged account, in a locked-down container. Its tool, `tt-minecraftd`, starts, stops, sets up, and updates the server, and puts the safe settings back in place before every start.

<!-- The first release run draws assets/demo.gif. Uncomment this then.
<p align="center"><img src="assets/demo.gif" alt="tt-minecraftd showing its version, the server status, and the unsafe settings that stop a start"></p>
-->

<!-- TOC ignore:true -->
## Table of contents

<!-- TOC -->

- [Why](#why)
- [How to connect](#how-to-connect)
- [Features](#features)
- [Update agent](#update-agent)
- [Installing](#installing)
- [Building from source](#building-from-source)
- [Copyright and license](#copyright-and-license)

<!-- /TOC -->

## Why

## How to connect

Players on Java and Bedrock, on the LAN or over the internet, can follow [how-to-connect.md](how-to-connect.md).

## Features

## Update agent

`update --plugins` knows where to get Paper, Geyser, and Floodgate. For your other plugins it can ask an AI agent where the newer builds are. The agent only suggests. `tt-minecraftd` downloads and checks each jar it finds, and you say yes to each one yourself, even with `--yes`. It's off until `agent` is set in `/etc/tt-minecraftd/config.shcl`. [design.md](project/design.md#update-agent) has the details.

- When PaperMC's download API can't be reached, the agent can find the Paper build too, checked the same way. The container image isn't refreshed then, since only PaperMC says which Java a version needs. Before `update --mc-version`, it says which of your plugins have a build for the new version. That report changes nothing.

- Run `tt-minecraftd update` as yourself, not with sudo. It asks the agent as you, then runs itself through sudo. Started as root, it skips the agent and says so.

- An API key comes from your environment, never the config, since everyone can read the config. It's `TT_MINECRAFTD_AGENT_KEY`, or else the usual `OPENAI_API_KEY` or `ANTHROPIC_API_KEY`. A local server needs none.

- Jars and hash pages may only come from the hosts in `agentHosts`. The default has Hangar, Modrinth, GitHub, GeyserMC, and PaperMC. A jar from somewhere else is listed as skipped, with its host, so you can add it.

- OpenAI, or any server with the same chat API:

	~~~text
	agent: openai
	agentModel: gpt-5-mini
	~~~

- OpenRouter:

	~~~text
	agent: openai
	agentURL: https://openrouter.ai/api/v1
	agentModel: openai/gpt-5-mini
	~~~

- A local Ollama. A small model may not find builds reliably, so check what it says.

	~~~text
	agent: openai
	agentURL: http://127.0.0.1:11434/v1
	agentModel: qwen2.5:7b-instruct
	~~~

- Anthropic's API: `agent: anthropic`, with `agentModel` set to one of its model names.

- A subscription CLI, which reads the question on stdin and prints its answer. Give it web reads and nothing else, such as no shell and no file writes. These flags change between versions, so check them against your CLI's own docs.

	~~~text
	agent: command
	agentCommand: [codex, --search, exec, --sandbox, read-only, --skip-git-repo-check, --ephemeral, -]
	~~~

	~~~text
	agent: command
	agentCommand: [gemini, --approval-mode, plan, --output-format, text]
	~~~

- `--set agent=none` skips the agent for one run, and `update --plugins --dry-run` shows what it suggests without downloading anything.

## Installing

## Building from source

Release builds are reproducible: a tag always builds to the same bytes. The pipeline builds linux/amd64 twice, from 2 folders, and fails when the checksums differ.

To check a release against its source, run this from a clone:

~~~bash
cicd/utility/verify-release.bash v1.0.0-alpha.1
~~~

It rebuilds each Linux binary from the tag, with the Go version and flags the release was built with, and compares it with the release's `SHA256SUMS`. It needs git, curl, and Go 1.21 or newer, which fetches the pinned Go version itself.

## Copyright and license

> Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)<br />
> Licensed under the [GNU General Public License v2.0 or later](https://spdx.org/licenses/GPL-2.0-or-later.html)<br />
> SPDX-License-Identifier: `GPL-2.0-or-later` <br />
> No warranty.<br />
> tt-minecraft-safeserver™ is a [trademark](trademark.md) of t00mietum.
<!--
> Licensed under the [MIT License](https://mit-license.org/)<br />
> SPDX-License-Identifier: `MIT`.<br />
	- Most permissive and least protective

> Licensed under the [GNU General Public License v2.0](https://www.gnu.org/licenses/gpl-2.0.html).

> Licensed under the [GNU General Public License v2.0 or later](https://spdx.org/licenses/GPL-2.0-or-later.html).
> SPDX-License-Identifier: `GPL-2.0-or-later`<br />

> Licensed under the [GNU General Public License v3](https://www.gnu.org/licenses/gpl-3.0.en.html) license.

> Licensed under the [Mozilla Public License 2.0](https://mozilla.org/MPL/2.0/).

> Licensed under the [Apache 2.0 License](https://www.apache.org/licenses/LICENSE-2.0.html)
> SPDX-License-Identifier: `Apache-2.0`<br />
	- Permissive
	- Patent protection clause
	- Requires attribution
	- No contribute back
	- SaaS loophole
-->
