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
