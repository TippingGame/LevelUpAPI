/**
 * Admin API Keys API endpoints
 * Handles API key management for administrators
 */

import { apiClient } from '../client'
import type { ApiKey, ApiKeyGroupRoute } from '@/types'

export interface UpdateApiKeyGroupResult {
  api_key: ApiKey
  auto_granted_group_access: boolean
  granted_group_id?: number
  granted_group_name?: string
}

/**
 * Update an API key's group binding
 * @param id - API Key ID
 * @param groupId - Group ID (0 to unbind, positive to bind, null/undefined to skip)
 * @returns Updated API key with auto-grant info
 */
export async function updateApiKeyGroup(id: number, groupId: number | null): Promise<UpdateApiKeyGroupResult> {
  const { data } = await apiClient.put<UpdateApiKeyGroupResult>(`/admin/api-keys/${id}`, {
    group_id: groupId === null ? 0 : groupId
  })
  return data
}

/**
 * Update an API key's multi-group route bindings.
 * @param id - API Key ID
 * @param groupId - Primary group ID (0/null to unbind)
 * @param groupRoutes - Route list ([] to unbind)
 */
export async function updateApiKeyGroupRoutes(
  id: number,
  groupId: number | null,
  groupRoutes: ApiKeyGroupRoute[]
): Promise<UpdateApiKeyGroupResult> {
  const { data } = await apiClient.put<UpdateApiKeyGroupResult>(`/admin/api-keys/${id}`, {
    group_id: groupId === null ? 0 : groupId,
    group_routes: groupRoutes
  })
  return data
}

export const apiKeysAPI = {
  updateApiKeyGroup,
  updateApiKeyGroupRoutes
}

export default apiKeysAPI
