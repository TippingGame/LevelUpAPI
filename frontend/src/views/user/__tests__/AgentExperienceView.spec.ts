import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const currentDir = dirname(fileURLToPath(import.meta.url))
const viewSource = readFileSync(resolve(currentDir, '../AgentExperienceView.vue'), 'utf8')
const routerSource = readFileSync(resolve(currentDir, '../../../router/index.ts'), 'utf8')
const sidebarSource = readFileSync(resolve(currentDir, '../../../components/layout/AppSidebar.vue'), 'utf8')

describe('LevelUpAgent online experience integration', () => {
  it('uses the requested project and release links without disruptive promotion', () => {
    expect(viewSource).toContain("const PROJECT_URL = 'https://github.com/TippingGame/LevelUpAgent'")
    expect(viewSource).toContain("const DOWNLOAD_URL = 'https://github.com/TippingGame/LevelUpAgent/releases'")
    expect(viewSource).not.toContain('showDownloadDialog')
    expect(viewSource).not.toContain('setInterval')
  })

  it('keeps API key and model selection while fixing the experience to Q&A mode', () => {
    expect(viewSource).toContain('v-model="selectedKeyId"')
    expect(viewSource).toContain('v-model="selectedModel"')
    expect(viewSource).toContain("t('agentExperience.qaMode')")
    expect(viewSource).not.toContain('v-model="mode"')
  })

  it('is available as an authenticated user navigation entry', () => {
    expect(routerSource).toMatch(/path: '\/agent'[\s\S]*?requiresAuth: true[\s\S]*?titleKey: 'agentExperience\.title'/)
    expect(sidebarSource).toContain("{ path: '/agent', label: t('nav.agentExperience'), icon: AgentIcon }")
  })
})
