import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get } = vi.hoisted(() => ({ get: vi.fn() }))

vi.mock('@/api/client', () => ({ apiClient: { get } }))

import { getUserOverview, listRebateRecords } from '@/api/admin/affiliates'

describe('admin affiliate records API', () => {
  beforeEach(() => get.mockReset())

  it('passes record filters to the usage-settlement rebate endpoint', async () => {
    const response = { items: [], total: 0, page: 2, page_size: 50 }
    get.mockResolvedValue({ data: response })

    await expect(listRebateRecords({
      page: 2,
      page_size: 50,
      search: 'alice',
      start_at: '2026-07-01',
      end_at: '2026-07-15',
      sort_by: 'rebate_amount',
      sort_order: 'desc',
      timezone: 'Asia/Shanghai',
    })).resolves.toEqual(response)

    expect(get).toHaveBeenCalledWith('/admin/affiliates/rebates', {
      params: {
        page: 2,
        page_size: 50,
        search: 'alice',
        start_at: '2026-07-01',
        end_at: '2026-07-15',
        sort_by: 'rebate_amount',
        sort_order: 'desc',
        timezone: 'Asia/Shanghai',
      },
    })
  })

  it('loads a user overview from the nested user route', async () => {
    const response = { user_id: 42 }
    get.mockResolvedValue({ data: response })

    await expect(getUserOverview(42)).resolves.toEqual(response)
    expect(get).toHaveBeenCalledWith('/admin/affiliates/users/42/overview')
  })
})
