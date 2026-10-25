'use strict';

// Writes a DeepSeek-only Codex model catalog to argv[2]. Entries reuse a model from the
// installed Codex's bundled catalog, so prompts and tool settings match that Codex version,
// and override what DeepSeek's Responses API supports.
const fs = require('node:fs');
const { execFileSync } = require('node:child_process');

const output = process.argv[2];
if (!output) {
  console.error('usage: codex-deepseek-catalog.cjs OUTPUT');
  process.exit(2);
}

const bundled = JSON.parse(execFileSync('codex', ['debug', 'models'], { encoding: 'utf8' }));
const models = Array.isArray(bundled.models) ? bundled.models : [];
const template = models.find((model) => model.slug === 'gpt-5.5') || models.find((model) => model.visibility === 'list');
if (!template) {
  console.error('no bundled Codex model to base the DeepSeek catalog on');
  process.exit(1);
}

const reasoningLevels = [
  { effort: 'none', description: 'Disable thinking mode' },
  { effort: 'low', description: 'Lighter reasoning for fast responses' },
  { effort: 'high', description: 'High reasoning effort (DeepSeek default)' },
  { effort: 'max', description: 'Maximum reasoning effort' }
];

function deepSeekModel(slug, displayName, description, priority) {
  return {
    ...template,
    slug,
    display_name: displayName,
    description,
    priority,
    visibility: 'list',
    context_window: 1048576,
    max_context_window: 1048576,
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

const catalog = {
  models: [
    deepSeekModel('deepseek-flash', 'DeepSeek Flash', 'Fast DeepSeek agentic coding model', 1),
    deepSeekModel('deepseek-v4-pro', 'DeepSeek V4 Pro', 'DeepSeek flagship model', 2)
  ]
};
fs.writeFileSync(output, JSON.stringify(catalog), { mode: 0o600 });
