import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  fetchAgentExperienceModels,
  streamAgentExperienceChat,
} from '@/api/agentExperience'

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('agent experience gateway client', () => {
  it('loads, deduplicates, and sorts models with the selected API key', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      object: 'list',
      data: [{ id: 'z-model' }, { id: 'a-model' }, { id: 'z-model' }, { id: '' }],
    }), { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(fetchAgentExperienceModels('sk-user')).resolves.toEqual([
      { id: 'a-model' },
      { id: 'z-model' },
    ])
    expect(fetchMock).toHaveBeenCalledWith('/v1/models', expect.objectContaining({
      headers: { Authorization: 'Bearer sk-user' },
    }))
  })

  it('streams text deltas from chat completions', async () => {
    const encoder = new TextEncoder()
    const body = new ReadableStream({
      start(controller) {
        controller.enqueue(encoder.encode('data: {"choices":[{"delta":{"content":"你"}}]}\n\n'))
        controller.enqueue(encoder.encode('data: {"choices":[{"delta":{"content":"好"}}]}\n\ndata: [DONE]\n\n'))
        controller.close()
      },
    })
    const fetchMock = vi.fn().mockResolvedValue(new Response(body, {
      status: 200,
      headers: { 'Content-Type': 'text/event-stream' },
    }))
    vi.stubGlobal('fetch', fetchMock)
    const deltas: string[] = []

    await streamAgentExperienceChat({
      apiKey: 'sk-user',
      model: 'test-model',
      messages: [{ role: 'user', content: 'hello' }],
      onDelta: (delta) => deltas.push(delta),
    })

    expect(deltas).toEqual(['你', '好'])
    expect(fetchMock).toHaveBeenCalledWith('/v1/chat/completions', expect.objectContaining({
      method: 'POST',
      headers: expect.objectContaining({ Authorization: 'Bearer sk-user' }),
    }))
  })

  it('returns the gateway error message', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({
      error: { message: 'Invalid API key' },
    }), { status: 401 })))

    await expect(fetchAgentExperienceModels('bad-key')).rejects.toThrow('Invalid API key')
  })

  it('surfaces errors sent inside an established SSE stream', async () => {
    const encoder = new TextEncoder()
    const body = new ReadableStream({
      start(controller) {
        controller.enqueue(encoder.encode('data: {"error":{"message":"Upstream unavailable"}}\n\n'))
        controller.close()
      },
    })
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(body, { status: 200 })))

    await expect(streamAgentExperienceChat({
      apiKey: 'sk-user',
      model: 'test-model',
      messages: [{ role: 'user', content: 'hello' }],
      onDelta: vi.fn(),
    })).rejects.toThrow('Upstream unavailable')
  })
})
