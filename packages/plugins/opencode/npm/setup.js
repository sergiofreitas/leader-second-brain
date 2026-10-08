#!/usr/bin/env node
// OpenCode plugin setup — copies skills and config to .opencode/
const fs = require('fs');
const path = require('path');

const home = process.env.HOME || process.env.USERPROFILE;
const targetDir = path.join(process.cwd(), '.opencode');

// Copy skills
const skillsSource = path.join(__dirname, '..', '..', '..', 'skills');
const skillsTarget = path.join(targetDir, 'skills');

if (fs.existsSync(skillsSource)) {
  fs.mkdirSync(skillsTarget, { recursive: true });
  for (const skill of fs.readdirSync(skillsSource)) {
    const src = path.join(skillsSource, skill);
    const dst = path.join(skillsTarget, skill);
    if (fs.statSync(src).isDirectory()) {
      fs.cpSync(src, dst, { recursive: true });
      console.log(`  skill: ${skill}`);
    }
  }
}

// Copy AGENTS.md
const agentsSrc = path.join(__dirname, '..', '..', '.opencode', 'AGENTS.md');
const agentsDst = path.join(targetDir, 'AGENTS.md');
if (fs.existsSync(agentsSrc)) {
  fs.mkdirSync(targetDir, { recursive: true });
  fs.copyFileSync(agentsSrc, agentsDst);
  console.log('  AGENTS.md copied');
}

console.log('Second Brain skills installed to .opencode/');
console.log('Next: add the MCP server to your opencode.json:');
console.log(JSON.stringify({
  mcp: {
    "second-brain": {
      type: "local",
      command: ["second-brain", "--config", "~/.second-brain/config.yaml"],
      enabled: true
    }
  }
}, null, 2));
