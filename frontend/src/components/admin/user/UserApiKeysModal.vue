<template>
  <BaseDialog :show="show" :title="t('admin.users.userApiKeys')" width="wide" @close="handleClose">
    <div v-if="user" class="space-y-4">
      <div class="flex items-center gap-3 rounded-xl bg-gray-50 p-4 dark:bg-dark-700">
        <div class="flex h-10 w-10 items-center justify-center rounded-full bg-primary-100 dark:bg-primary-900/30">
          <span class="text-lg font-medium text-primary-700 dark:text-primary-300">{{ user.email.charAt(0).toUpperCase() }}</span>
        </div>
        <div><p class="font-medium text-gray-900 dark:text-white">{{ user.email }}</p><p class="text-sm text-gray-500 dark:text-dark-400">{{ user.username }}</p></div>
      </div>
      <div v-if="loading" class="flex justify-center py-8"><svg class="h-8 w-8 animate-spin text-primary-500" fill="none" viewBox="0 0 24 24"><circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle><path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path></svg></div>
      <div v-else-if="apiKeys.length === 0" class="py-8 text-center"><p class="text-sm text-gray-500">{{ t('admin.users.noApiKeys') }}</p></div>
      <div v-else ref="scrollContainerRef" class="max-h-96 space-y-3 overflow-y-auto" @scroll="closeGroupSelector">
        <div v-for="key in apiKeys" :key="key.id" class="rounded-xl border border-gray-200 bg-white p-4 dark:border-dark-600 dark:bg-dark-800">
          <div class="flex items-start justify-between">
            <div class="min-w-0 flex-1">
              <div class="mb-1 flex items-center gap-2"><span class="font-medium text-gray-900 dark:text-white">{{ key.name }}</span><span :class="['badge text-xs', key.status === 'active' ? 'badge-success' : 'badge-danger']">{{ key.status }}</span></div>
              <p class="truncate font-mono text-sm text-gray-500">{{ key.key.substring(0, 20) }}...{{ key.key.substring(key.key.length - 8) }}</p>
            </div>
          </div>
          <div class="mt-3 flex flex-wrap gap-4 text-xs text-gray-500">
            <div class="flex items-center gap-1">
              <span>{{ t('admin.users.group') }}:</span>
              <button
                :ref="(el) => setGroupButtonRef(key.id, el)"
                @click="openGroupSelector(key)"
                class="-mx-1 -my-0.5 flex max-w-full cursor-pointer flex-wrap items-center gap-1 rounded-md px-1 py-0.5 transition-colors hover:bg-gray-100 dark:hover:bg-dark-700"
                :disabled="updatingKeyIds.has(key.id)"
              >
                <GroupBadge
                  v-for="group in keyRouteGroups(key)"
                  :key="group.id"
                  :name="group.name"
                  :platform="group.platform"
                  :scope="group.scope"
                  :subscription-type="group.subscription_type"
                  :rate-multiplier="group.rate_multiplier"
                />
                <span v-if="keyRouteGroups(key).length === 0" class="text-gray-400 italic">{{ t('admin.users.none') }}</span>
                <svg v-if="updatingKeyIds.has(key.id)" class="h-3 w-3 animate-spin text-primary-500" fill="none" viewBox="0 0 24 24"><circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle><path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path></svg>
                <svg v-else class="h-3 w-3 text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M8.25 15L12 18.75 15.75 15m-7.5-6L12 5.25 15.75 9" /></svg>
              </button>
            </div>
            <div class="flex items-center gap-1"><span>{{ t('admin.users.columns.created') }}: {{ formatDateTime(key.created_at) }}</span></div>
          </div>
        </div>
      </div>
    </div>
  </BaseDialog>

  <!-- Group Selector Dropdown -->
  <Teleport to="body">
    <div
      v-if="groupSelectorKeyId !== null && dropdownPosition"
      ref="dropdownRef"
      class="animate-in fade-in slide-in-from-top-2 fixed z-[100000020] w-[380px] overflow-hidden rounded-xl bg-white shadow-lg ring-1 ring-black/5 duration-200 dark:bg-dark-800 dark:ring-white/10"
      :style="{ top: dropdownPosition.top + 'px', left: dropdownPosition.left + 'px' }"
    >
      <div class="border-b border-gray-100 p-2 dark:border-dark-700">
        <input
          v-model="groupSearchQuery"
          type="text"
          class="w-full rounded-lg border border-gray-200 bg-gray-50 px-3 py-1.5 text-sm text-gray-900 outline-none focus:border-primary-300 focus:ring-1 focus:ring-primary-300 dark:border-dark-600 dark:bg-dark-700 dark:text-white dark:focus:border-primary-600 dark:focus:ring-primary-600"
          :placeholder="t('admin.users.searchGroups')"
          @click.stop
        />
      </div>
      <div class="max-h-72 overflow-y-auto p-1.5">
        <label
          v-for="group in filteredGroups"
          :key="group.id"
          :class="[
            'flex w-full cursor-pointer items-center gap-2 rounded-lg px-3 py-2 text-sm transition-colors',
            selectedDropdownGroupIds.includes(group.id)
              ? 'bg-primary-50 dark:bg-primary-900/20'
              : 'hover:bg-gray-100 dark:hover:bg-dark-700'
          ]"
        >
          <input
            type="checkbox"
            class="h-4 w-4 shrink-0 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-500"
            :checked="selectedDropdownGroupIds.includes(group.id)"
            @change="handleDropdownGroupToggle(group.id, ($event.target as HTMLInputElement).checked)"
            @click.stop
          />
          <span
            v-if="selectedDropdownGroupIds.includes(group.id)"
            class="inline-flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-primary-50 text-xs font-medium text-primary-700 dark:bg-primary-900/30 dark:text-primary-300"
          >
            {{ selectedDropdownGroupIds.indexOf(group.id) + 1 }}
          </span>
          <GroupOptionItem
            :name="group.name"
            :platform="group.platform"
            :scope="group.scope"
            :subscription-type="group.subscription_type"
            :rate-multiplier="group.rate_multiplier"
            :description="group.description"
            :selected="selectedDropdownGroupIds.includes(group.id)"
          />
        </label>
        <div v-if="filteredGroups.length === 0" class="py-4 text-center text-sm text-gray-400 dark:text-gray-500">
          没有匹配的分组
        </div>
      </div>
      <div class="flex items-center justify-between gap-2 border-t border-gray-100 p-2 dark:border-dark-700">
        <button type="button" class="btn btn-secondary text-sm" @click="clearDropdownGroups">
          清空
        </button>
        <button
          type="button"
          class="btn btn-primary text-sm"
          :disabled="!selectedKeyForGroup || updatingKeyIds.has(selectedKeyForGroup.id)"
          @click="applyDropdownGroups"
        >
          保存
        </button>
      </div>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted, type ComponentPublicInstance } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { adminAPI } from '@/api/admin'
import { formatDateTime } from '@/utils/format'
import type { AdminUser, AdminGroup, ApiKey, ApiKeyGroupRoute } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'
import GroupBadge from '@/components/common/GroupBadge.vue'
import GroupOptionItem from '@/components/common/GroupOptionItem.vue'

const props = defineProps<{ show: boolean; user: AdminUser | null }>()
const emit = defineEmits(['close'])
const { t } = useI18n()
const appStore = useAppStore()

const apiKeys = ref<ApiKey[]>([])
const allGroups = ref<AdminGroup[]>([])
const loading = ref(false)
const updatingKeyIds = ref(new Set<number>())
const groupSelectorKeyId = ref<number | null>(null)
const dropdownPosition = ref<{ top: number; left: number } | null>(null)
const dropdownRef = ref<HTMLElement | null>(null)
const scrollContainerRef = ref<HTMLElement | null>(null)
const groupButtonRefs = ref<Map<number, HTMLElement>>(new Map())
const dropdownGroupIds = ref<number[]>([])
const groupSearchQuery = ref('')

const selectedKeyForGroup = computed(() => {
  if (groupSelectorKeyId.value === null) return null
  return apiKeys.value.find((k) => k.id === groupSelectorKeyId.value) || null
})

const groupById = computed(() => {
  const map = new Map<number, AdminGroup>()
  for (const group of allGroups.value) {
    map.set(group.id, group)
  }
  return map
})

const uniqueGroupIds = (ids: number[]): number[] => {
  const seen = new Set<number>()
  const orderedIds: number[] = []
  for (const id of ids) {
    if (seen.has(id)) continue
    seen.add(id)
    orderedIds.push(id)
  }
  return orderedIds
}

const routeGroupIdsFromKey = (key: ApiKey): number[] => {
  const routeIds = (key.group_routes || [])
    .filter((route) => route.enabled !== false)
    .slice()
    .sort((a, b) => {
      if (a.priority !== b.priority) return a.priority - b.priority
      if (a.weight !== b.weight) return b.weight - a.weight
      return a.group_id - b.group_id
    })
    .map((route) => route.group_id)
  if (routeIds.length > 0) return uniqueGroupIds(routeIds)
  return key.group_id ? [key.group_id] : []
}

const keyRouteGroups = (key: ApiKey): AdminGroup[] => {
  return routeGroupIdsFromKey(key)
    .map((id) => {
      const routeGroup = (key.group_routes || []).find((route) => route.group_id === id)?.group
      return groupById.value.get(id) || (key.group?.id === id ? key.group as AdminGroup : null) || (routeGroup as AdminGroup | undefined) || null
    })
    .filter((group): group is AdminGroup => !!group)
}

const selectedDropdownGroupIds = computed(() => dropdownGroupIds.value)
const selectedDropdownPlatform = computed(() => {
  const firstGroup = selectedDropdownGroupIds.value
    .map((id) => groupById.value.get(id))
    .find((group): group is AdminGroup => !!group)
  return firstGroup?.platform ?? null
})

const filteredGroups = computed(() => {
  const query = groupSearchQuery.value.trim().toLowerCase()
  return allGroups.value.filter((group) => {
    if (selectedDropdownPlatform.value && group.platform !== selectedDropdownPlatform.value) return false
    if (!query) return true
    return group.name.toLowerCase().includes(query) ||
      (group.description || '').toLowerCase().includes(query)
  })
})

const normalizeGroupRoutes = (groupIds: number[]): ApiKeyGroupRoute[] | null => {
  const orderedGroupIds = uniqueGroupIds(groupIds)
  const selectedGroups = orderedGroupIds
    .map((id) => groupById.value.get(id))
    .filter((group): group is AdminGroup => !!group)
  if (selectedGroups.length !== orderedGroupIds.length) {
    appStore.showError(t('admin.users.groupChangeFailed'))
    return null
  }
  const platform = selectedGroups[0]?.platform
  if (platform && selectedGroups.some((group) => group.platform !== platform)) {
    appStore.showError('只能选择同一开发商的分组')
    return null
  }
  return orderedGroupIds.map((groupId, index) => ({
    group_id: groupId,
    priority: (index + 1) * 100,
    weight: 1,
    enabled: true,
    cooldown_seconds: 30
  }))
}

const resolvePrimaryGroupId = (routes: ApiKeyGroupRoute[]): number | null => routes[0]?.group_id ?? null

const setGroupButtonRef = (keyId: number, el: Element | ComponentPublicInstance | null) => {
  if (el instanceof HTMLElement) {
    groupButtonRefs.value.set(keyId, el)
  } else {
    groupButtonRefs.value.delete(keyId)
  }
}

watch(() => props.show, (v) => {
  if (v && props.user) {
    load()
    loadGroups()
  } else {
    closeGroupSelector()
  }
})

const load = async () => {
  if (!props.user) return
  loading.value = true
  groupButtonRefs.value.clear()
  try {
    const res = await adminAPI.users.getUserApiKeys(props.user.id)
    apiKeys.value = res.items || []
  } catch (error) {
    console.error('Failed to load API keys:', error)
  } finally {
    loading.value = false
  }
}

const loadGroups = async () => {
  try {
    const groups = await adminAPI.groups.getAll()
    allGroups.value = groups
  } catch (error) {
    console.error('Failed to load groups:', error)
  }
}

const DROPDOWN_HEIGHT = 272 // max-h-64 = 16rem = 256px + padding
const DROPDOWN_GAP = 4

const openGroupSelector = (key: ApiKey) => {
  if (groupSelectorKeyId.value === key.id) {
    closeGroupSelector()
  } else {
    const buttonEl = groupButtonRefs.value.get(key.id)
    if (buttonEl) {
      const rect = buttonEl.getBoundingClientRect()
      const spaceBelow = window.innerHeight - rect.bottom
      const openUpward = spaceBelow < DROPDOWN_HEIGHT && rect.top > spaceBelow
      dropdownPosition.value = {
        top: openUpward ? rect.top - DROPDOWN_HEIGHT - DROPDOWN_GAP : rect.bottom + DROPDOWN_GAP,
        left: rect.left
      }
    }
    groupSelectorKeyId.value = key.id
    dropdownGroupIds.value = routeGroupIdsFromKey(key)
    groupSearchQuery.value = ''
  }
}

const closeGroupSelector = () => {
  groupSelectorKeyId.value = null
  dropdownPosition.value = null
  dropdownGroupIds.value = []
  groupSearchQuery.value = ''
}

const handleDropdownGroupToggle = (groupId: number, checked: boolean) => {
  const group = groupById.value.get(groupId)
  if (checked && selectedDropdownPlatform.value && group && group.platform !== selectedDropdownPlatform.value) {
    appStore.showError('只能选择同一开发商的分组')
    return
  }
  dropdownGroupIds.value = checked
    ? uniqueGroupIds([...dropdownGroupIds.value, groupId])
    : dropdownGroupIds.value.filter((id) => id !== groupId)
}

const clearDropdownGroups = () => {
  dropdownGroupIds.value = []
}

const applyDropdownGroups = async () => {
  const key = selectedKeyForGroup.value
  if (!key) return
  const routes = normalizeGroupRoutes(dropdownGroupIds.value)
  if (routes === null) return
  const primaryGroupId = resolvePrimaryGroupId(routes)

  updatingKeyIds.value.add(key.id)
  try {
    const result = await adminAPI.apiKeys.updateApiKeyGroupRoutes(key.id, primaryGroupId, routes)
    // Update local data
    const idx = apiKeys.value.findIndex((k) => k.id === key.id)
    if (idx !== -1) {
      apiKeys.value[idx] = result.api_key
    }
    closeGroupSelector()
    if (result.auto_granted_group_access && result.granted_group_name) {
      appStore.showSuccess(t('admin.users.groupChangedWithGrant', { group: result.granted_group_name }))
    } else {
      appStore.showSuccess(t('admin.users.groupChangedSuccess'))
    }
  } catch (error: any) {
    appStore.showError(error?.message || t('admin.users.groupChangeFailed'))
  } finally {
    updatingKeyIds.value.delete(key.id)
  }
}

const handleKeyDown = (event: KeyboardEvent) => {
  if (event.key === 'Escape' && groupSelectorKeyId.value !== null) {
    event.stopPropagation()
    closeGroupSelector()
  }
}

const handleClickOutside = (event: MouseEvent) => {
  const target = event.target as HTMLElement
  if (dropdownRef.value && !dropdownRef.value.contains(target)) {
    // Check if the click is on one of the group trigger buttons
    for (const el of groupButtonRefs.value.values()) {
      if (el.contains(target)) return
    }
    closeGroupSelector()
  }
}

const handleClose = () => {
  closeGroupSelector()
  emit('close')
}

onMounted(() => {
  document.addEventListener('click', handleClickOutside)
  document.addEventListener('keydown', handleKeyDown, true)
})

onUnmounted(() => {
  document.removeEventListener('click', handleClickOutside)
  document.removeEventListener('keydown', handleKeyDown, true)
})
</script>
