import { describe, expect, it, vi } from 'vitest'

vi.mock('@/api/admin/accounts', () => ({
  getAntigravityDefaultModelMapping: vi.fn()
}))

import { buildModelMappingObject, getModelsByPlatform, getPresetMappingsByPlatform } from '../useModelWhitelist'

describe('useModelWhitelist', () => {
  it('openai 模型列表包含 GPT-5.4 官方快照', () => {
    const models = getModelsByPlatform('openai')

    expect(models).toContain('gpt-5.4')
    expect(models).toContain('gpt-5.4-mini')
    expect(models).toContain('gpt-5.4-2026-03-05')
  })

  it('openai 模型列表和预设包含 GPT-5.6 三个计费变体', () => {
    const models = getModelsByPlatform('openai')
    expect(models).toEqual(expect.arrayContaining([
      'gpt-5.6',
      'gpt-5.6-sol',
      'gpt-5.6-terra',
      'gpt-5.6-luna'
    ]))

    const presets = getPresetMappingsByPlatform('openai')
    expect(presets).toEqual(expect.arrayContaining([
      expect.objectContaining({ from: 'gpt-5.6-sol', to: 'gpt-5.6-sol' }),
      expect.objectContaining({ from: 'gpt-5.6-terra', to: 'gpt-5.6-terra' }),
      expect.objectContaining({ from: 'gpt-5.6-luna', to: 'gpt-5.6-luna' })
    ]))
  })

  it('Grok 模型列表包含最新文本模型及别名，不暴露图片模型', () => {
    const models = getModelsByPlatform('grok')

    expect(models).toEqual(expect.arrayContaining([
      'grok-4.5',
      'grok-4.3',
      'grok-build-0.1',
      'grok-composer-2.5-fast',
      'grok-4.20-0309-reasoning',
      'grok-4.20-0309-non-reasoning',
      'grok-4.20-multi-agent-0309',
      'grok-latest',
      'grok-4.5-latest'
    ]))
    expect(getModelsByPlatform('xai')).toEqual(models)
    expect(models.some((model) => model.startsWith('grok-imagine'))).toBe(false)
    expect(models).not.toContain('grok-2-image')
  })

  it('openai 模型列表不再暴露已下线的 ChatGPT 登录 Codex 模型', () => {
    const models = getModelsByPlatform('openai')

    expect(models).not.toContain('gpt-5')
    expect(models).not.toContain('gpt-5.1')
    expect(models).not.toContain('gpt-5.1-codex')
    expect(models).not.toContain('gpt-5.1-codex-max')
    expect(models).not.toContain('gpt-5.1-codex-mini')
    expect(models).not.toContain('gpt-5.2-codex')
  })

  it('antigravity 模型列表包含图片模型兼容项', () => {
    const models = getModelsByPlatform('antigravity')

    expect(models).toContain('gemini-2.5-flash-image')
    expect(models).toContain('gemini-3.1-flash-image')
    expect(models).toContain('gemini-3-pro-image')
  })

  it('Claude/Antigravity 模型列表包含 Opus 4.8', () => {
    expect(getModelsByPlatform('anthropic')).toContain('claude-opus-4-8')
    expect(getModelsByPlatform('antigravity')).toContain('claude-opus-4-8')
  })

  it('Claude/Antigravity 模型列表包含 Fable 5', () => {
    expect(getModelsByPlatform('anthropic')).toContain('claude-fable-5')
    expect(getModelsByPlatform('antigravity')).toContain('claude-fable-5')
  })

  it('Claude 模型列表和预设映射包含 Sonnet 5', () => {
    expect(getModelsByPlatform('anthropic')).toContain('claude-sonnet-5')

    expect(getPresetMappingsByPlatform('anthropic')).toContainEqual(
      expect.objectContaining({
        label: 'Sonnet 5',
        from: 'claude-sonnet-5',
        to: 'claude-sonnet-5'
      })
    )
    expect(getPresetMappingsByPlatform('bedrock')).toContainEqual(
      expect.objectContaining({
        label: 'Sonnet 5',
        from: 'claude-sonnet-5',
        to: 'us.anthropic.claude-sonnet-5-v1'
      })
    )
  })

  it('gemini 模型列表包含原生生图模型', () => {
    const models = getModelsByPlatform('gemini')

    expect(models).toContain('gemini-2.5-flash-image')
    expect(models).toContain('gemini-3.1-flash-image')
  })

  it('gemini 模型列表包含 Gemini 3.5 Flash', () => {
    expect(getModelsByPlatform('gemini')).toContain('gemini-3.5-flash')
  })

  it('antigravity 模型列表会把新的 Gemini 图片模型排在前面', () => {
    const models = getModelsByPlatform('antigravity')

    expect(models.indexOf('gemini-3.1-flash-image')).toBeLessThan(
      models.indexOf('gemini-2.5-flash-image')
    )
  })

  it('whitelist 模式会忽略通配符条目', () => {
    const mapping = buildModelMappingObject('whitelist', ['claude-*', 'gemini-2.5-flash'], [])
    expect(mapping).toEqual({
      'gemini-2.5-flash': 'gemini-2.5-flash'
    })
  })

  it('whitelist 模式会保留 GPT-5.4 官方快照的精确映射', () => {
    const mapping = buildModelMappingObject('whitelist', ['gpt-5.4-2026-03-05'], [])

    expect(mapping).toEqual({
      'gpt-5.4-2026-03-05': 'gpt-5.4-2026-03-05'
    })
  })

  it('whitelist keeps GPT-5.4 mini exact mappings', () => {
    const mapping = buildModelMappingObject('whitelist', ['gpt-5.4-mini'], [])

    expect(mapping).toEqual({
      'gpt-5.4-mini': 'gpt-5.4-mini'
    })
  })
})
