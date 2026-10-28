'use strict';

// Writes a Codex model catalog listing only the model server's models (argv[3], comma
// separated) to argv[2]. Entries reuse a model from the installed Codex's bundled catalog,
// so prompts and tool settings match that Codex version.
const fs = require('node:fs');
const { execFileSync } = require('node:child_process');

const output = process.argv[2];
const slugs = (process.argv[3] || '').split(',').filter((slug) => /^[A-Za-z0-9][A-Za-z0-9._:/@+-]{0,127}$/.test(slug));
if (!output || slugs.length === 0) {
  console.error('usage: codex-model-catalog.cjs OUTPUT MODEL[,MODEL...]');
  process.exit(2);
}

const bundled = JSON.parse(execFileSync('codex', ['debug', 'models'], { encoding: 'utf8' }));
const models = Array.isArray(bundled.models) ? bundled.models : [];
const template = models.find((model) => model.slug === 'gpt-5.5') || models.find((model) => model.visibility === 'list');
if (!template) {
  console.error('no bundled Codex model to base the catalog on');
  process.exit(1);
}

const reasoningLevels = [
  { effort: 'none', description: 'Disable thinking mode' },
  { effort: 'low', description: 'Lighter reasoning for fast responses' },
  { effort: 'high', description: 'High reasoning effort' },
  { effort: 'max', description: 'Maximum reasoning effort' }
];

function serverModel(slug, priority) {
  return {
    ...template,
    slug,
    display_name: slug,
    description: 'Model from the workspace model server',
    priority,
    visibility: 'list',
    default_reasoning_level: 'high',
    supported_reasoning_levels: reasoningLevels,
    shell_type: 'shell_command',
    tool_mode: null,
    use_responses_lite: false,
    web_search_tool_type: 'text',
    experimental_supported_tools: [],
    prefer_websockets: false,
    upgrade: null,
    availability_nux: null
  };
}

const catalog = { models: slugs.map((slug, index) => serverModel(slug, index + 1)) };
fs.writeFileSync(output, JSON.stringify(catalog), { mode: 0o600 });
