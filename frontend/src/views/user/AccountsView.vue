<template>
  <AppLayout>
    <div v-if="!canManageSharedAccounts" class="space-y-5">
      <section class="overflow-hidden rounded-lg border border-gray-200 bg-white shadow-sm dark:border-dark-700 dark:bg-dark-900">
        <div class="grid gap-0 lg:grid-cols-[minmax(0,1.2fr)_minmax(18rem,0.8fr)]">
          <div class="p-5 sm:p-6">
            <div class="flex flex-wrap items-start justify-between gap-4">
              <div>
                <p class="text-sm font-semibold text-primary-600 dark:text-primary-300">共享号主进度</p>
                <h2 class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">历史兑换满 {{ formatGameCoins(sharedOwnerThreshold, { minimumFractionDigits: 0, maximumFractionDigits: 0 }) }} 自动开启</h2>
                <p class="mt-2 max-w-2xl text-sm leading-6 text-gray-500 dark:text-gray-400">
                  开启后可接入共享号池、管理自有共享账号，并使用余额提现与收款码功能。
                </p>
              </div>
              <span
                class="rounded-full px-3 py-1 text-sm font-medium"
                :class="sharedOwnerStatus?.mode === 'manual_off'
                  ? 'bg-red-50 text-red-700 dark:bg-red-900/25 dark:text-red-300'
                  : 'bg-amber-50 text-amber-700 dark:bg-amber-900/25 dark:text-amber-300'"
              >
                {{ sharedOwnerStatusText }}
              </span>
            </div>

            <div class="mt-6">
              <div class="flex items-center justify-between gap-3 text-sm">
                <span class="font-medium text-gray-700 dark:text-gray-300">累计历史兑换</span>
                <span class="font-semibold text-gray-900 dark:text-white">
                  {{ formatGameCoins(sharedOwnerTotal) }} / {{ formatGameCoins(sharedOwnerThreshold, { minimumFractionDigits: 0, maximumFractionDigits: 0 }) }}
                </span>
              </div>
              <div class="mt-3 h-3 overflow-hidden rounded-full bg-gray-100 dark:bg-dark-800">
                <div
                  class="h-full rounded-full bg-gradient-to-r from-emerald-500 via-sky-500 to-primary-500 transition-all duration-500"
                  :style="{ width: `${sharedOwnerProgressPercent}%` }"
                ></div>
              </div>
              <div class="mt-3 flex flex-wrap items-center justify-between gap-2 text-xs text-gray-500 dark:text-gray-400">
                <span>当前进度 {{ sharedOwnerProgressPercent }}%</span>
                <span v-if="sharedOwnerStatus?.mode === 'manual_off'">管理员已手动关闭，自动达标暂不生效。</span>
                <span v-else>还差 {{ formatGameCoins(sharedOwnerRemaining) }} 即可自动开启。</span>
              </div>
            </div>
          </div>

          <div class="border-t border-gray-200 bg-gray-50 p-5 dark:border-dark-700 dark:bg-dark-950/40 sm:p-6 lg:border-l lg:border-t-0">
            <div class="grid gap-3">
              <div class="rounded-lg bg-white p-4 ring-1 ring-gray-100 dark:bg-dark-900 dark:ring-dark-700">
                <p class="text-xs text-gray-500 dark:text-gray-400">已累计</p>
                <p class="mt-1 text-xl font-semibold text-gray-900 dark:text-white">{{ formatGameCoins(sharedOwnerTotal) }}</p>
              </div>
              <div class="rounded-lg bg-white p-4 ring-1 ring-gray-100 dark:bg-dark-900 dark:ring-dark-700">
                <p class="text-xs text-gray-500 dark:text-gray-400">解锁差额</p>
                <p class="mt-1 text-xl font-semibold text-gray-900 dark:text-white">{{ formatGameCoins(sharedOwnerRemaining) }}</p>
              </div>
              <div class="rounded-lg bg-white p-4 ring-1 ring-gray-100 dark:bg-dark-900 dark:ring-dark-700">
                <p class="text-xs text-gray-500 dark:text-gray-400">可用功能</p>
                <p class="mt-1 text-sm font-medium text-gray-900 dark:text-white">共享号池、账号导入、收益提现</p>
              </div>
            </div>
          </div>
        </div>
      </section>
    </div>

    <TablePageLayout v-else>
      <template #actions>
        <div class="flex flex-col gap-3 xl:flex-row xl:items-center xl:justify-between">
          <div class="flex flex-wrap items-center gap-2 text-xs">
            <span
              v-if="revenuePolicyLoading"
              class="rounded-md border border-gray-200 bg-white px-3 py-2 text-gray-500 shadow-sm dark:border-dark-700 dark:bg-dark-900 dark:text-dark-400"
            >
              {{ t('common.loading') }}
            </span>
            <span
              v-else-if="revenuePolicyError"
              class="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-red-600 shadow-sm dark:border-red-900/50 dark:bg-red-900/20 dark:text-red-300"
            >
              {{ t('userAccounts.revenuePolicy.loadFailed') }}
            </span>
            <template v-else>
              <span
                v-for="item in revenuePolicyItems"
                :key="item.key"
                class="inline-flex items-center gap-1.5 rounded-md border border-gray-200 bg-white px-3 py-2 text-gray-600 shadow-sm dark:border-dark-700 dark:bg-dark-900 dark:text-dark-300"
              >
                <span>{{ item.label }}</span>
                <span class="font-semibold text-gray-900 dark:text-white">{{ item.value }}</span>
              </span>
            </template>
          </div>

          <div class="flex flex-wrap items-center justify-end gap-3">
            <button
              type="button"
              class="btn btn-secondary"
              :disabled="loading"
              :title="t('common.refresh')"
              @click="refreshAccountsPage"
            >
              <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
            </button>
            <button
              type="button"
              class="btn btn-secondary"
              :disabled="selectedCount === 0"
              @click="openBulkEditModal"
            >
              <Icon name="edit" size="md" class="mr-2" />
              {{ t('admin.accounts.bulkActions.edit') }}
            </button>
            <button
              type="button"
              class="btn btn-secondary"
              :disabled="exportingData"
              @click="openExportDataDialog"
            >
              <Icon name="download" size="md" class="mr-2" />
              {{ selectedCount > 0 ? t('userAccounts.exportSelected') : t('userAccounts.exportAccounts') }}
            </button>
            <button type="button" class="btn btn-secondary" @click="showImportModal = true">
              <Icon name="upload" size="md" class="mr-2" />
              {{ t('userAccounts.importAccounts') }}
            </button>
            <button type="button" class="btn btn-primary" @click="openCreateModal">
              <Icon name="plus" size="md" class="mr-2" />
              {{ t('userAccounts.createAccount') }}
            </button>
          </div>
        </div>
      </template>

      <template #filters>
        <div class="flex flex-wrap items-center gap-3">
          <SearchInput
            v-model="filterSearch"
            :placeholder="t('userAccounts.searchPlaceholder')"
            class="w-full sm:w-64"
            @search="onFilterChange"
          />
          <Select
            :model-value="filterPlatform"
            class="w-44"
            :options="platformFilterOptions"
            @update:model-value="onPlatformFilterChange"
          />
          <Select
            :model-value="filterType"
            class="w-40"
            :options="typeFilterOptions"
            @update:model-value="onTypeFilterChange"
          />
          <Select
            :model-value="filterStatus"
            class="w-40"
            :options="statusFilterOptions"
            @update:model-value="onStatusFilterChange"
          />
          <Select
            :model-value="filterGroupId"
            class="w-44"
            :options="groupFilterOptions"
            @update:model-value="onGroupFilterChange"
          />
        </div>
      </template>

      <template #table>
        <div
          v-if="selectedCount > 0"
          class="mb-4 flex flex-wrap items-center justify-between gap-3 rounded-lg bg-primary-50 p-3 dark:bg-primary-900/20"
        >
          <div class="flex flex-wrap items-center gap-2">
            <span class="text-sm font-medium text-primary-900 dark:text-primary-100">
              {{ t('admin.accounts.bulkActions.selected', { count: selectedCount }) }}
            </span>
            <button
              type="button"
              class="text-xs font-medium text-primary-700 hover:text-primary-800 dark:text-primary-300 dark:hover:text-primary-200"
              @click="selectVisible"
            >
              {{ t('admin.accounts.bulkActions.selectCurrentPage') }}
            </button>
            <span class="text-gray-300 dark:text-primary-800">/</span>
            <button
              type="button"
              class="text-xs font-medium text-primary-700 hover:text-primary-800 dark:text-primary-300 dark:hover:text-primary-200"
              @click="clearSelection"
            >
              {{ t('admin.accounts.bulkActions.clear') }}
            </button>
          </div>
          <div class="flex flex-wrap items-center gap-2">
            <button type="button" class="btn btn-danger btn-sm" @click="openBulkDeleteDialog">
              {{ t('admin.accounts.bulkActions.delete') }}
            </button>
            <button type="button" class="btn btn-secondary btn-sm" @click="bulkRefreshTokens">
              {{ t('admin.accounts.bulkActions.refreshToken') }}
            </button>
            <button type="button" class="btn btn-secondary btn-sm" @click="bulkRevalidatePublicShare">
              {{ t('userAccounts.bulkRevalidateShare') }}
            </button>
            <button type="button" class="btn btn-success btn-sm" @click="bulkToggleSchedulable(true)">
              {{ t('admin.accounts.bulkActions.enableScheduling') }}
            </button>
            <button type="button" class="btn btn-warning btn-sm" @click="bulkToggleSchedulable(false)">
              {{ t('admin.accounts.bulkActions.disableScheduling') }}
            </button>
            <button type="button" class="btn btn-secondary btn-sm" @click="openBulkEditModal">
              {{ t('admin.accounts.bulkActions.edit') }}
            </button>
            <button type="button" class="btn btn-primary btn-sm" @click="openBulkEditModal">
              {{ t('admin.accounts.bulkEdit.submit') }}
            </button>
          </div>
        </div>
        <DataTable
          :columns="columns"
          :data="accounts"
          :loading="loading"
          row-key="id"
          :server-side-sort="true"
          default-sort-key="created_at"
          default-sort-order="desc"
          :estimate-row-height="72"
          :overscan="5"
          @sort="handleSort"
        >
          <template #header-select>
            <input
              type="checkbox"
              class="h-4 w-4 cursor-pointer rounded border-gray-300 text-primary-600 focus:ring-primary-500"
              :checked="allVisibleSelected"
              @click.stop
              @change="toggleSelectAllVisible($event)"
            />
          </template>
          <template #cell-select="{ row }">
            <input
              type="checkbox"
              class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
              :checked="isSelected(row.id)"
              @change="toggleSelection(row.id)"
            />
          </template>
          <template #cell-name="{ row, value }">
            <div class="flex min-w-[180px] flex-col">
              <span class="font-medium text-gray-900 dark:text-white">{{ value }}</span>
              <span
                v-if="row.extra?.email_address"
                class="max-w-[220px] truncate text-xs text-gray-500 dark:text-gray-400"
                :title="row.extra.email_address"
              >
                {{ row.extra.email_address }}
              </span>
            </div>
          </template>

          <template #cell-platform_type="{ row }">
            <div class="flex flex-wrap items-center gap-1">
              <PlatformTypeBadge
                :platform="row.platform"
                :type="row.type"
                :plan-type="row.credentials?.plan_type"
                :privacy-mode="row.extra?.privacy_mode"
                :subscription-expires-at="row.credentials?.subscription_expires_at"
              />
              <span
                v-if="getOpenAICompactLabel(row)"
                :class="['inline-block rounded px-1.5 py-0.5 text-[10px] font-medium', getOpenAICompactClass(row)]"
                :title="getOpenAICompactTitle(row)"
              >
                {{ getOpenAICompactLabel(row) }}
              </span>
              <span
                v-if="getAntigravityTierLabel(row)"
                :class="['inline-block rounded px-1.5 py-0.5 text-[10px] font-medium', getAntigravityTierClass(row)]"
              >
                {{ getAntigravityTierLabel(row) }}
              </span>
            </div>
          </template>

          <template #cell-share="{ row }">
            <div class="flex flex-col gap-1">
              <span :class="shareModeBadgeClass(row.share_mode)">
                {{ shareModeLabel(row.share_mode) }}
              </span>
			  <div v-if="row.share_mode === 'public'" class="flex items-center gap-1">
                <span :class="shareStatusBadgeClass(row.share_status)" :title="shareStatusTitle(row)">
                  {{ shareStatusLabel(row.share_status) }}
                </span>
                <button
                  v-if="canRevalidatePublicShare(row)"
                  type="button"
                  class="inline-flex h-5 w-5 items-center justify-center rounded text-amber-600 transition-colors hover:bg-amber-50 hover:text-amber-700 disabled:cursor-not-allowed disabled:opacity-60 dark:text-amber-300 dark:hover:bg-amber-900/30 dark:hover:text-amber-200"
                  :disabled="revalidatingShareId === row.id"
                  :title="t('userAccounts.revalidateShare')"
                  @click="revalidatePublicShare(row)"
                >
                  <Icon
                    name="refresh"
                    size="xs"
                    :class="revalidatingShareId === row.id ? 'animate-spin' : ''"
                  />
                </button>
                <div v-if="shareStatusHelpText(row)" class="group/share relative inline-flex">
                  <Icon
                    name="infoCircle"
                    size="xs"
                    class="cursor-help text-amber-500 transition-colors group-hover/share:text-amber-600 dark:text-amber-300 dark:group-hover/share:text-amber-200"
                  />
                  <div
                    class="pointer-events-none invisible absolute left-0 top-full z-[100] mt-1.5 w-72 max-w-[calc(100vw-2rem)] rounded-lg bg-gray-900 px-3 py-2 text-xs text-white opacity-0 shadow-xl transition-all duration-200 group-hover/share:visible group-hover/share:opacity-100 dark:bg-gray-800"
                  >
                    <div class="mb-1 font-medium text-amber-200">
                      {{ t('userAccounts.shareValidationTitle') }}
                    </div>
                    <div class="whitespace-pre-wrap break-words leading-relaxed text-gray-200">
                      {{ shareStatusHelpText(row) }}
                    </div>
                    <div
                      class="absolute bottom-full left-3 border-[6px] border-transparent border-b-gray-900 dark:border-b-gray-800"
                    ></div>
                  </div>
                </div>
              </div>
            </div>
          </template>

          <template #cell-capacity="{ row }">
            <AccountCapacityCell :account="row" />
          </template>

          <template #cell-status="{ row }">
            <AccountStatusIndicator :account="row" @show-temp-unsched="handleShowTempUnsched" />
          </template>

          <template #cell-schedulable="{ row }">
            <button
              type="button"
              class="relative inline-flex h-5 w-9 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50 dark:focus:ring-offset-dark-800"
              :class="row.schedulable ? 'bg-primary-500 hover:bg-primary-600' : 'bg-gray-200 hover:bg-gray-300 dark:bg-dark-600 dark:hover:bg-dark-500'"
              :disabled="togglingSchedulableId === row.id"
              :title="row.schedulable ? t('admin.accounts.schedulableEnabled') : t('admin.accounts.schedulableDisabled')"
              @click="toggleSchedulable(row)"
            >
              <span
                class="pointer-events-none inline-block h-4 w-4 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out"
                :class="row.schedulable ? 'translate-x-4' : 'translate-x-0'"
              />
            </button>
          </template>

          <template #cell-today_stats="{ row }">
            <AccountTodayStatsCell
              :stats="todayStatsByAccountId[String(row.id)] ?? null"
              :loading="todayStatsLoading"
              :error="todayStatsError"
            />
          </template>

          <template #cell-groups="{ row }">
            <AccountGroupsCell :groups="row.groups" :max-display="4" />
          </template>

          <template #cell-usage="{ row }">
            <AccountUsageCell
              :account="row"
              :today-stats="todayStatsByAccountId[String(row.id)] ?? null"
              :today-stats-loading="todayStatsLoading"
              :usage-loader="accountsAPI.getUsage"
              usage-cache-scope="user"
              account-scope="user"
              :manual-refresh-token="usageManualRefreshToken"
            />
          </template>

          <template #cell-priority="{ row }">
            <span class="text-sm text-gray-700 dark:text-gray-300">
              {{ row.private_priority ?? row.priority }}
            </span>
          </template>

          <template #cell-last_used_at="{ value }">
            <span class="text-sm text-gray-500 dark:text-dark-400">{{ formatRelativeTime(value) }}</span>
          </template>

          <template #cell-expires_at="{ row, value }">
            <div class="flex flex-col items-start gap-1">
              <span class="text-sm text-gray-500 dark:text-dark-400">{{ formatExpiresAt(value) }}</span>
              <div v-if="isExpired(value) || (row.auto_pause_on_expired && value)" class="flex items-center gap-1">
                <span
                  v-if="isExpired(value)"
                  class="inline-flex items-center rounded-md bg-amber-100 px-2 py-0.5 text-xs font-medium text-amber-700 dark:bg-amber-900/30 dark:text-amber-300"
                >
                  {{ t('admin.accounts.expired') }}
                </span>
                <span
                  v-if="row.auto_pause_on_expired && value"
                  class="inline-flex items-center rounded-md bg-emerald-100 px-2 py-0.5 text-xs font-medium text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300"
                >
                  {{ t('admin.accounts.autoPauseOnExpired') }}
                </span>
              </div>
            </div>
          </template>

          <template #cell-notes="{ value }">
            <span v-if="value" :title="value" class="block max-w-xs truncate text-sm text-gray-600 dark:text-gray-300">
              {{ value }}
            </span>
            <span v-else class="text-sm text-gray-400 dark:text-dark-500">-</span>
          </template>

          <template #cell-actions="{ row }">
            <div class="flex items-center justify-end gap-1">
              <button
                type="button"
                class="rounded-lg p-2 text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-700 dark:text-dark-300 dark:hover:bg-dark-700 dark:hover:text-white"
                :title="t('common.edit')"
                @click="openEditModal(row)"
              >
                <Icon name="edit" size="sm" />
              </button>
              <button
                type="button"
                class="rounded-lg p-2 text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-700 dark:text-dark-300 dark:hover:bg-dark-700 dark:hover:text-white"
                :disabled="togglingStatusId === row.id"
                :title="isAccountActive(row) ? t('userAccounts.disable') : t('userAccounts.enable')"
                @click="toggleAccountStatus(row)"
              >
                <Icon :name="isAccountActive(row) ? 'ban' : 'checkCircle'" size="sm" />
              </button>
              <button
                type="button"
                class="rounded-lg p-2 text-red-500 transition-colors hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20"
                :title="t('common.delete')"
                @click="openDeleteDialog(row)"
              >
                <Icon name="trash" size="sm" />
              </button>
              <button
                type="button"
                class="rounded-lg p-2 text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-700 dark:text-dark-300 dark:hover:bg-dark-700 dark:hover:text-white"
                :title="t('common.more')"
                @click="openActionMenu(row, $event)"
              >
                <Icon name="more" size="sm" />
              </button>
            </div>
          </template>

          <template #empty>
            <EmptyState
              :title="t('userAccounts.noAccountsYet')"
              :description="t('userAccounts.createFirstAccount')"
              :action-text="t('userAccounts.createAccount')"
              @action="openCreateModal"
            />
          </template>
        </DataTable>
      </template>

      <template #pagination>
        <Pagination
          v-if="pagination.total > 0"
          :page="pagination.page"
          :total="pagination.total"
          :page-size="pagination.page_size"
          @update:page="handlePageChange"
          @update:pageSize="handlePageSizeChange"
        />
      </template>
    </TablePageLayout>

    <CreateAccountModal
      v-if="canManageSharedAccounts"
      :show="showCreateModal"
      :proxies="userProxies"
      :groups="modalGroups"
      account-scope="user"
      :allow-proxy="true"
      :allow-billing-rate="false"
      @close="showCreateModal = false"
      @created="handleAccountCreated"
    />

    <EditAccountModal
      v-if="canManageSharedAccounts"
      :show="showEditModal"
      :account="editingAccount"
      :proxies="[]"
      :groups="modalGroups"
      account-scope="user"
      :allow-proxy="false"
      :allow-billing-rate="false"
      @close="closeEditModal"
      @updated="handleAccountUpdated"
    />

    <BulkEditAccountModal
      v-if="canManageSharedAccounts"
      :show="showBulkEditModal"
      :account-ids="selectedIds"
      :selected-platforms="selectedPlatforms"
      :selected-types="selectedTypes"
      :proxies="[]"
      :groups="modalGroups"
      account-scope="user"
      :allow-proxy="false"
      :allow-billing-rate="false"
      :allow-base-url="false"
      @close="showBulkEditModal = false"
      @updated="handleBulkAccountsUpdated"
    />

    <ConfirmDialog
      v-if="canManageSharedAccounts"
      :show="showDeleteDialog"
      :title="t('userAccounts.deleteAccount')"
      :message="deleteConfirmMessage"
      :confirm-text="t('common.delete')"
      :cancel-text="t('common.cancel')"
      danger
      @confirm="deleteAccount"
      @cancel="closeDeleteDialog"
    />

    <ConfirmDialog
      v-if="canManageSharedAccounts"
      :show="showBulkDeleteDialog"
      :title="t('admin.accounts.bulkDeleteTitle')"
      :message="bulkDeleteConfirmMessage"
      :confirm-text="t('common.delete')"
      :cancel-text="t('common.cancel')"
      danger
      @confirm="bulkDeleteAccounts"
      @cancel="closeBulkDeleteDialog"
    />

    <ConfirmDialog
      v-if="canManageSharedAccounts"
      :show="showExportDataDialog"
      :title="t('userAccounts.exportAccounts')"
      :message="t('userAccounts.exportConfirmMessage')"
      :confirm-text="exportingData ? t('userAccounts.exporting') : t('userAccounts.exportConfirm')"
      :cancel-text="t('common.cancel')"
      @confirm="handleExportData"
      @cancel="showExportDataDialog = false"
    />

    <ImportAccountsModal
      v-if="canManageSharedAccounts"
      :show="showImportModal"
      @close="showImportModal = false"
      @imported="handleAccountsImported"
    />

    <AccountTestModal
      v-if="canManageSharedAccounts"
      :show="showTestModal"
      :account="testingAccount"
      account-scope="user"
      test-endpoint-base="/api/v1/accounts"
      @close="closeTestModal"
    />

    <AccountStatsModal
      v-if="canManageSharedAccounts"
      :show="showStatsModal"
      :account="statsAccount"
      :stats-loader="accountsAPI.getStats"
      @close="closeStatsModal"
    />

    <ReAuthAccountModal
      v-if="canManageSharedAccounts"
      :show="showReAuthModal"
      :account="reAuthAccount"
      account-scope="user"
      @close="closeReAuthModal"
      @reauthorized="handleAccountReauthorized"
    />

    <UserAccountActionMenu
      v-if="canManageSharedAccounts"
      :show="actionMenu.show"
      :account="actionMenu.account"
      :position="actionMenu.position"
      @close="actionMenu.show = false"
      @test="handleTest"
      @stats="handleViewStats"
      @reauth="handleReAuth"
      @refresh-token="handleRefreshToken"
      @set-privacy="handleSetPrivacy"
      @verify-level="handleVerifyLevel"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { accountsAPI, userGroupsAPI } from '@/api'
import type { SharedOwnerRevenuePolicy } from '@/api/accounts'
import type { AccountBatchTask, UserAccountVerifyLevelTarget } from '@/api/accounts'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { getPersistedPageSize } from '@/composables/usePersistedPageSize'
import { useTableSelection } from '@/composables/useTableSelection'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Select from '@/components/common/Select.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import Icon from '@/components/icons/Icon.vue'
import PlatformTypeBadge from '@/components/common/PlatformTypeBadge.vue'
import AccountCapacityCell from '@/components/account/AccountCapacityCell.vue'
import AccountStatusIndicator from '@/components/account/AccountStatusIndicator.vue'
import AccountGroupsCell from '@/components/account/AccountGroupsCell.vue'
import AccountUsageCell from '@/components/account/AccountUsageCell.vue'
import AccountTodayStatsCell from '@/components/account/AccountTodayStatsCell.vue'
import CreateAccountModal from '@/components/account/CreateAccountModal.vue'
import EditAccountModal from '@/components/account/EditAccountModal.vue'
import BulkEditAccountModal from '@/components/account/BulkEditAccountModal.vue'
import AccountStatsModal from '@/components/account/AccountStatsModal.vue'
import ReAuthAccountModal from '@/components/account/ReAuthAccountModal.vue'
import AccountTestModal from '@/components/account/AccountTestModal.vue'
import UserAccountActionMenu from '@/components/account/UserAccountActionMenu.vue'
import ImportAccountsModal from '@/components/user/ImportAccountsModal.vue'
import { ACCOUNT_STATUS_FILTER_OPTIONS } from '@/constants/account'
import type { Account, AccountPlatform, AccountType, AdminGroup, Group, Proxy, WindowStats } from '@/types'
import type { Column } from '@/components/common/types'
import { formatDateTime, formatRelativeTime } from '@/utils/format'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatGameCoins } from '@/utils/gameCurrency'

type UserAccountStatus = 'active' | 'disabled'

const { t, locale } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()

const accounts = ref<Account[]>([])
const groups = ref<Group[]>([])
const userProxies = ref<Proxy[]>([])
const userProxiesLoading = ref(false)
const loading = ref(false)
const showCreateModal = ref(false)
const showEditModal = ref(false)
const showImportModal = ref(false)
const showBulkEditModal = ref(false)
const showDeleteDialog = ref(false)
const showBulkDeleteDialog = ref(false)
const showExportDataDialog = ref(false)
const showTestModal = ref(false)
const showStatsModal = ref(false)
const showReAuthModal = ref(false)
const editingAccount = ref<Account | null>(null)
const accountToDelete = ref<Account | null>(null)
const testingAccount = ref<Account | null>(null)
const statsAccount = ref<Account | null>(null)
const reAuthAccount = ref<Account | null>(null)
const togglingStatusId = ref<number | null>(null)
const togglingSchedulableId = ref<number | null>(null)
const revalidatingShareId = ref<number | null>(null)
const verifyingLevelId = ref<number | null>(null)
const usageManualRefreshToken = ref(0)
const todayStatsByAccountId = ref<Record<string, WindowStats>>({})
const todayStatsLoading = ref(false)
const todayStatsError = ref<string | null>(null)
const todayStatsReqSeq = ref(0)
const revenuePolicy = ref<SharedOwnerRevenuePolicy | null>(null)
const revenuePolicyLoading = ref(true)
const revenuePolicyError = ref(false)
const exportingData = ref(false)
let abortController: AbortController | null = null
const actionMenu = reactive<{
  show: boolean
  account: Account | null
  position: { top: number; left: number } | null
}>({
  show: false,
  account: null,
  position: null
})

const pagination = ref({
  page: 1,
  page_size: getPersistedPageSize(),
  total: 0,
  pages: 0
})

const sortState = ref({
  sort_by: 'created_at',
  sort_order: 'desc' as 'asc' | 'desc'
})

const filterSearch = ref('')
const filterPlatform = ref('')
const filterType = ref('')
const filterStatus = ref('')
const filterGroupId = ref<string | number>('')
const activeBatchTaskPolls = new Set<number>()
let isUnmounted = false
const ACCOUNT_BATCH_TASK_POLL_TIMEOUT_MS = 30 * 60 * 1000

const sharedOwnerStatus = computed(() => authStore.user?.shared_account_owner_status ?? null)
const canManageSharedAccounts = computed(() => authStore.canManageUserAccounts)
const sharedOwnerThreshold = computed(() => Number(sharedOwnerStatus.value?.threshold ?? 100))
const sharedOwnerTotal = computed(() => Number(sharedOwnerStatus.value?.total_recharged ?? authStore.user?.total_recharged ?? 0))
const sharedOwnerRemaining = computed(() => Math.max(0, Number(sharedOwnerStatus.value?.remaining ?? sharedOwnerThreshold.value - sharedOwnerTotal.value)))
const sharedOwnerProgressPercent = computed(() => {
  const progress = sharedOwnerStatus.value?.progress ?? (sharedOwnerThreshold.value > 0 ? sharedOwnerTotal.value / sharedOwnerThreshold.value : 0)
  return Math.max(0, Math.min(100, Math.round(progress * 100)))
})
const sharedOwnerStatusText = computed(() => {
  switch (sharedOwnerStatus.value?.mode) {
    case 'manual_off':
      return '管理员已关闭'
    case 'manual_on':
      return '管理员已开启'
    case 'auto':
      return '已自动开启'
    default:
      return '未开启'
  }
})

const modalGroups = computed(() => groups.value as unknown as AdminGroup[])

const {
  selectedIds,
  selectedCount,
  allVisibleSelected,
  isSelected,
  toggle: toggleSelection,
  clear: clearSelection,
  selectVisible,
  toggleVisible
} = useTableSelection<Account>({
  rows: accounts,
  getId: (account) => account.id
})

const selectedAccounts = computed(() => accounts.value.filter((account) => isSelected(account.id)))
const selectedPlatforms = computed<AccountPlatform[]>(() => [
  ...new Set(selectedAccounts.value.map((account) => account.platform))
])
const selectedTypes = computed<AccountType[]>(() => [
  ...new Set(selectedAccounts.value.map((account) => account.type))
])

const columns = computed<Column[]>(() => [
  { key: 'select', label: '', sortable: false, class: 'w-10' },
  { key: 'name', label: t('admin.accounts.columns.name'), sortable: true },
  { key: 'platform_type', label: t('admin.accounts.columns.platformType'), sortable: false, class: 'min-w-[150px]' },
  { key: 'share', label: t('userAccounts.share'), sortable: false },
  { key: 'capacity', label: t('admin.accounts.columns.capacity'), sortable: false },
  { key: 'status', label: t('admin.accounts.columns.status'), sortable: true },
  { key: 'schedulable', label: t('admin.accounts.columns.schedulable'), sortable: true },
  { key: 'today_stats', label: t('admin.accounts.columns.todayStats'), sortable: false },
  { key: 'groups', label: t('admin.accounts.columns.groups'), sortable: false, class: 'min-w-[180px] max-w-[220px]' },
  { key: 'usage', label: t('admin.accounts.columns.usageWindows'), sortable: false, class: 'min-w-[180px]' },
  { key: 'priority', label: t('admin.accounts.privatePriority'), sortable: true },
  { key: 'last_used_at', label: t('admin.accounts.columns.lastUsed'), sortable: true },
  { key: 'expires_at', label: t('admin.accounts.columns.expiresAt'), sortable: true },
  { key: 'notes', label: t('admin.accounts.columns.notes'), sortable: false },
  { key: 'actions', label: t('admin.accounts.columns.actions'), sortable: false }
])

const platformOptions = computed<Array<{ value: AccountPlatform; label: string }>>(() => [
  { value: 'anthropic', label: 'Anthropic' },
  { value: 'openai', label: 'OpenAI' },
  { value: 'gemini', label: 'Gemini' },
  { value: 'antigravity', label: 'Antigravity' }
])

const typeOptions = computed<Array<{ value: AccountType; label: string }>>(() => [
  { value: 'oauth', label: 'OAuth' },
  { value: 'setup-token', label: 'Setup Token' },
  { value: 'apikey', label: 'API Key' },
  { value: 'upstream', label: 'Upstream' },
  { value: 'bedrock', label: 'Bedrock' }
])

const platformFilterOptions = computed(() => [
  { value: '', label: t('userAccounts.allPlatforms') },
  ...platformOptions.value
])

const typeFilterOptions = computed(() => [
  { value: '', label: t('userAccounts.allTypes') },
  ...typeOptions.value
])

const statusFilterOptions = computed(() => [
  ...ACCOUNT_STATUS_FILTER_OPTIONS.map(({ value, labelKey }) => ({ value, label: t(labelKey) }))
])

const groupFilterOptions = computed(() => [
  { value: '', label: t('keys.allGroups') },
  { value: -1, label: t('userAccounts.privateDefaultGroupOnly') },
  ...groups.value.map((group) => ({
    value: group.id,
    label: group.name
  }))
])

const revenuePolicyItems = computed(() => {
  return [
    {
      key: 'owner',
      label: t('userAccounts.revenuePolicy.sharedOwnerShare'),
      value: formatPolicyRatio(revenuePolicy.value?.shared_owner_share_ratio)
    },
    {
      key: 'privateGroupCommission',
      label: t('userAccounts.revenuePolicy.privateGroupCommission'),
      value: formatPolicyRatio(revenuePolicy.value?.private_group_commission_rate)
    }
  ]
})

const deleteConfirmMessage = computed(() =>
  t('userAccounts.deleteConfirmMessage', { name: accountToDelete.value?.name ?? '' })
)

const bulkDeleteConfirmMessage = computed(() =>
  t('admin.accounts.bulkDeleteConfirm', { count: selectedCount.value })
)

function buildAccountQueryFilters(): {
  search?: string
  platform?: string
  type?: string
  status?: string
  group_id?: string | number
  sort_by: string
  sort_order: 'asc' | 'desc'
} {
  const filters: {
    search?: string
    platform?: string
    type?: string
    status?: string
    group_id?: string | number
    sort_by: string
    sort_order: 'asc' | 'desc'
  } = {
    sort_by: sortState.value.sort_by,
    sort_order: sortState.value.sort_order
  }
  if (filterSearch.value.trim()) filters.search = filterSearch.value.trim()
  if (filterPlatform.value) filters.platform = filterPlatform.value
  if (filterType.value) filters.type = filterType.value
  if (filterStatus.value) filters.status = filterStatus.value
  if (filterGroupId.value !== '') filters.group_id = filterGroupId.value
  return filters
}

function formatPolicyRatio(value?: number | null): string {
  if (typeof value !== 'number' || !Number.isFinite(value)) return '--'
  const percent = Math.max(0, Math.min(100, value * 100))
  return `${percent.toLocaleString(locale.value, {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2
  })}%`
}

function formatExportTimestamp(): string {
  const now = new Date()
  const pad2 = (value: number) => String(value).padStart(2, '0')
  return `${now.getFullYear()}${pad2(now.getMonth() + 1)}${pad2(now.getDate())}${pad2(now.getHours())}${pad2(now.getMinutes())}${pad2(now.getSeconds())}`
}

function openExportDataDialog(): void {
  showExportDataDialog.value = true
}

async function handleExportData(): Promise<void> {
  if (exportingData.value) return
  exportingData.value = true
  try {
    const dataPayload = await accountsAPI.exportData(
      selectedIds.value.length > 0
        ? { ids: selectedIds.value }
        : { filters: buildAccountQueryFilters() }
    )
    const timestamp = formatExportTimestamp()
    const filename = `sub2api-user-account-${timestamp}.json`
    const blob = new Blob([JSON.stringify(dataPayload, null, 2)], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = filename
    link.click()
    URL.revokeObjectURL(url)
    appStore.showSuccess(t('userAccounts.exportSuccess'))
  } catch (error: any) {
    console.error('Failed to export user accounts:', error)
    appStore.showError(error?.response?.data?.message || error?.message || t('userAccounts.exportFailed'))
  } finally {
    exportingData.value = false
    showExportDataDialog.value = false
  }
}

function isAccountActive(account: Account): boolean {
  return account.status === 'active'
}

function isRefreshableAccount(account: Account): boolean {
  return account.type === 'oauth' || account.type === 'setup-token'
}

function buildDefaultTodayStats(): WindowStats {
  return {
    requests: 0,
    tokens: 0,
    cost: 0,
    standard_cost: 0,
    user_cost: 0
  }
}

async function refreshTodayStatsBatch(): Promise<void> {
  const accountIDs = accounts.value.map((account) => account.id)
  const reqSeq = ++todayStatsReqSeq.value
  if (accountIDs.length === 0) {
    todayStatsByAccountId.value = {}
    todayStatsError.value = null
    todayStatsLoading.value = false
    return
  }

  todayStatsLoading.value = true
  todayStatsError.value = null

  try {
    const result = await accountsAPI.getBatchTodayStats(accountIDs)
    if (reqSeq !== todayStatsReqSeq.value) return
    const serverStats = result.stats ?? {}
    const nextStats: Record<string, WindowStats> = {}
    for (const accountID of accountIDs) {
      const key = String(accountID)
      nextStats[key] = serverStats[key] ?? buildDefaultTodayStats()
    }
    todayStatsByAccountId.value = nextStats
  } catch (error) {
    if (reqSeq !== todayStatsReqSeq.value) return
    todayStatsError.value = 'Failed'
    console.error('Failed to load user account today stats:', error)
  } finally {
    if (reqSeq === todayStatsReqSeq.value) {
      todayStatsLoading.value = false
    }
  }
}

function getAntigravityTierFromRow(row: Account): string | null {
  if (row.platform !== 'antigravity') return null
  const loadCodeAssist = row.extra?.load_code_assist
  if (!loadCodeAssist || typeof loadCodeAssist !== 'object') return null
  const lca = loadCodeAssist as Record<string, unknown>
  const paid = lca.paidTier
  if (paid && typeof paid === 'object' && typeof (paid as Record<string, unknown>).id === 'string') {
    return (paid as Record<string, string>).id
  }
  const current = lca.currentTier
  if (current && typeof current === 'object' && typeof (current as Record<string, unknown>).id === 'string') {
    return (current as Record<string, string>).id
  }
  return null
}

function getAntigravityTierLabel(row: Account): string | null {
  const tier = getAntigravityTierFromRow(row)
  switch (tier) {
    case 'free-tier':
      return t('admin.accounts.tier.free')
    case 'g1-pro-tier':
      return t('admin.accounts.tier.pro')
    case 'g1-ultra-tier':
      return t('admin.accounts.tier.ultra')
    default:
      return null
  }
}

function getAntigravityTierClass(row: Account): string {
  const tier = getAntigravityTierFromRow(row)
  switch (tier) {
    case 'free-tier':
      return 'bg-gray-100 text-gray-600 dark:bg-gray-700 dark:text-gray-300'
    case 'g1-pro-tier':
      return 'bg-blue-100 text-blue-600 dark:bg-blue-900/40 dark:text-blue-300'
    case 'g1-ultra-tier':
      return 'bg-purple-100 text-purple-600 dark:bg-purple-900/40 dark:text-purple-300'
    default:
      return ''
  }
}

function getOpenAICompactState(row: Account): 'supported' | 'unsupported' | 'unknown' | null {
  if (row.platform !== 'openai' || (row.type !== 'oauth' && row.type !== 'apikey')) return null
  const mode = typeof row.extra?.openai_compact_mode === 'string' ? row.extra.openai_compact_mode : 'auto'
  if (mode === 'force_on') return 'supported'
  if (mode === 'force_off') return 'unsupported'
  if (typeof row.extra?.openai_compact_supported === 'boolean') {
    return row.extra.openai_compact_supported ? 'supported' : 'unsupported'
  }
  return 'unknown'
}

function getOpenAICompactLabel(row: Account): string | null {
  switch (getOpenAICompactState(row)) {
    case 'supported':
      return t('admin.accounts.openai.compactSupported')
    case 'unsupported':
      return t('admin.accounts.openai.compactUnsupported')
    case 'unknown':
      return t('admin.accounts.openai.compactUnknown')
    default:
      return null
  }
}

function getOpenAICompactClass(row: Account): string {
  switch (getOpenAICompactState(row)) {
    case 'supported':
      return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300'
    case 'unsupported':
      return 'bg-rose-100 text-rose-700 dark:bg-rose-900/40 dark:text-rose-300'
    case 'unknown':
      return 'bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-300'
    default:
      return ''
  }
}

function getOpenAICompactTitle(row: Account): string {
  const checkedAt = typeof row.extra?.openai_compact_checked_at === 'string' ? row.extra.openai_compact_checked_at : ''
  if (!checkedAt) return getOpenAICompactLabel(row) || ''
  return `${getOpenAICompactLabel(row)} | ${t('admin.accounts.openai.compactLastChecked')}: ${formatDateTime(new Date(checkedAt))}`
}

function shareModeLabel(mode?: string): string {
  return mode === 'public' ? t('userAccounts.publicMode') : t('userAccounts.privateMode')
}

function shareStatusLabel(status?: string): string {
  switch (status) {
    case 'pending':
      return t('userAccounts.pendingReview')
    case 'suspended':
      return t('userAccounts.suspended')
    default:
      return t('userAccounts.approved')
  }
}

function accountErrorMessage(row: Account): string {
  return typeof row.error_message === 'string' ? row.error_message.trim() : ''
}

function shareStatusHelpText(row: Account): string {
  if (row.share_mode !== 'public') return ''
  const reason = accountErrorMessage(row)
  switch (row.share_status) {
    case 'pending':
      return reason
        ? t('userAccounts.shareValidationFailed', { reason })
        : t('userAccounts.shareValidationPendingHint')
    case 'suspended':
      return reason ? t('userAccounts.shareValidationSuspended', { reason }) : ''
    default:
      return ''
  }
}

function shareStatusTitle(row: Account): string {
  return shareStatusHelpText(row) || shareStatusLabel(row.share_status)
}

function canRevalidatePublicShare(row: Account): boolean {
  return row.share_mode === 'public' && row.share_status !== 'approved'
}

function shareModeBadgeClass(mode?: string): string {
  const base = 'inline-flex w-fit rounded-md px-2 py-0.5 text-xs font-medium'
  return mode === 'public'
    ? `${base} bg-blue-50 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300`
    : `${base} bg-gray-100 text-gray-700 dark:bg-dark-700 dark:text-dark-200`
}

function shareStatusBadgeClass(status?: string): string {
  const base = 'inline-flex w-fit rounded-md px-2 py-0.5 text-xs font-medium'
  switch (status) {
    case 'pending':
      return `${base} bg-amber-50 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300`
    case 'suspended':
      return `${base} bg-red-50 text-red-700 dark:bg-red-900/30 dark:text-red-300`
    default:
      return `${base} bg-emerald-50 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300`
  }
}

function isAbortError(error: unknown): boolean {
  if (!error || typeof error !== 'object') return false
  const { name, code } = error as { name?: string; code?: string }
  return name === 'AbortError' || code === 'ERR_CANCELED'
}

async function loadAccounts(): Promise<void> {
  if (!canManageSharedAccounts.value) {
    abortController?.abort()
    accounts.value = []
    pagination.value.total = 0
    pagination.value.pages = 0
    loading.value = false
    return
  }
  abortController?.abort()
  const controller = new AbortController()
  abortController = controller
  const { signal } = controller
  loading.value = true

  try {
    const response = await accountsAPI.list(
      pagination.value.page,
      pagination.value.page_size,
      buildAccountQueryFilters(),
      { signal }
    )
    if (signal.aborted) return
    accounts.value = response.items
    pagination.value.total = response.total
    pagination.value.pages = response.pages
    await refreshTodayStatsBatch()
  } catch (error) {
    if (!isAbortError(error)) {
      console.error('Failed to load user accounts:', error)
      appStore.showError(t('userAccounts.failedToLoad'))
    }
  } finally {
    if (!signal.aborted) {
      loading.value = false
    }
  }
}

async function refreshCurrentUserBalance(): Promise<void> {
  try {
    await authStore.refreshUser()
  } catch (error) {
    console.error('Failed to refresh current user balance:', error)
    appStore.showError(extractApiErrorMessage(error, t('common.error')))
  }
}

async function refreshAccountsPage(): Promise<void> {
  const balanceRefresh = refreshCurrentUserBalance()
  const tasks: Array<Promise<void>> = []
  if (canManageSharedAccounts.value) {
    tasks.push(loadAccounts(), loadRevenuePolicy())
  }
  await Promise.all([...tasks, balanceRefresh])
}

async function loadGroups(): Promise<void> {
  if (!canManageSharedAccounts.value) {
    groups.value = []
    return
  }
  try {
    groups.value = await userGroupsAPI.getAvailable()
  } catch (error) {
    console.error('Failed to load available groups:', error)
  }
}

async function loadUserProxies(force = false): Promise<void> {
  if (!canManageSharedAccounts.value) return
  if (userProxiesLoading.value || (!force && userProxies.value.length > 0)) return
  userProxiesLoading.value = true
  try {
    userProxies.value = await accountsAPI.listProxies()
  } catch (error) {
    console.error('Failed to load user proxies:', error)
    appStore.showError(extractApiErrorMessage(error, t('userAccounts.importProxyLoadFailed')))
  } finally {
    userProxiesLoading.value = false
  }
}

async function loadRevenuePolicy(): Promise<void> {
  if (!canManageSharedAccounts.value) {
    revenuePolicy.value = null
    revenuePolicyError.value = false
    revenuePolicyLoading.value = false
    return
  }
  revenuePolicyLoading.value = true
  revenuePolicyError.value = false
  try {
    revenuePolicy.value = await accountsAPI.getRevenuePolicy()
  } catch (error) {
    revenuePolicy.value = null
    revenuePolicyError.value = true
    console.error('Failed to load account share revenue policy:', error)
  } finally {
    revenuePolicyLoading.value = false
  }
}

function openCreateModal(): void {
  showCreateModal.value = true
  void loadUserProxies()
}

function onFilterChange(): void {
  pagination.value.page = 1
  loadAccounts()
}

function onPlatformFilterChange(value: string | number | boolean | null): void {
  filterPlatform.value = String(value ?? '')
  onFilterChange()
}

function onTypeFilterChange(value: string | number | boolean | null): void {
  filterType.value = String(value ?? '')
  onFilterChange()
}

function onStatusFilterChange(value: string | number | boolean | null): void {
  filterStatus.value = String(value ?? '')
  onFilterChange()
}

function onGroupFilterChange(value: string | number | boolean | null): void {
  filterGroupId.value = value === null || typeof value === 'boolean' ? '' : value
  onFilterChange()
}

function toggleSelectAllVisible(event: Event): void {
  toggleVisible((event.target as HTMLInputElement).checked)
}

function handleSort(key: string, order: 'asc' | 'desc'): void {
  sortState.value.sort_by = key
  sortState.value.sort_order = order
  pagination.value.page = 1
  loadAccounts()
}

function handlePageChange(page: number): void {
  pagination.value.page = page
  loadAccounts()
}

function handlePageSizeChange(pageSize: number): void {
  pagination.value.page_size = pageSize
  pagination.value.page = 1
  loadAccounts()
}

function openEditModal(account: Account): void {
  editingAccount.value = account
  showEditModal.value = true
}

function closeEditModal(): void {
  showEditModal.value = false
  editingAccount.value = null
}

function openBulkEditModal(): void {
  if (selectedCount.value === 0) {
    appStore.showError(t('admin.accounts.bulkEdit.noSelection'))
    return
  }
  showBulkEditModal.value = true
}

function openBulkDeleteDialog(): void {
  if (selectedCount.value === 0) {
    appStore.showError(t('admin.accounts.bulkEdit.noSelection'))
    return
  }
  showBulkDeleteDialog.value = true
}

function closeBulkDeleteDialog(): void {
  showBulkDeleteDialog.value = false
}

async function handleAccountCreated(): Promise<void> {
  showCreateModal.value = false
  clearSelection()
  pagination.value.page = 1
  usageManualRefreshToken.value += 1
  await Promise.all([loadGroups(), loadAccounts()])
}

async function handleAccountUpdated(account: Account): Promise<void> {
  showEditModal.value = false
  editingAccount.value = null
  patchAccountInList(account)
  usageManualRefreshToken.value += 1
  await loadAccounts()
}

async function handleBulkAccountsUpdated(payload?: { async?: boolean; task?: AccountBatchTask }): Promise<void> {
  showBulkEditModal.value = false
  clearSelection()
  if (payload?.async && payload.task) {
    void pollUserAccountBatchTask(payload.task.id, (completed) => {
      if (completed.failed > 0) {
        appStore.showError(t('admin.accounts.bulkActions.partialSuccess', { success: completed.success, failed: completed.failed }))
      } else {
        appStore.showSuccess(t('userAccounts.bulkRevalidateCompleted', { count: completed.success }))
      }
    })
    return
  }
  usageManualRefreshToken.value += 1
  await Promise.all([loadGroups(), loadAccounts()])
}

async function handleAccountsImported(payload?: { close: boolean }): Promise<void> {
  if (payload?.close !== false) {
    showImportModal.value = false
  }
  clearSelection()
  pagination.value.page = 1
  usageManualRefreshToken.value += 1
  await loadAccounts()
}

function openDeleteDialog(account: Account): void {
  accountToDelete.value = account
  showDeleteDialog.value = true
}

function openActionMenu(account: Account, event: MouseEvent): void {
  actionMenu.account = account
  const target = event.currentTarget as HTMLElement | null
  const menuWidth = 208
  const menuHeight = 320
  const padding = 8
  const viewportWidth = window.innerWidth
  const viewportHeight = window.innerHeight

  if (target) {
    const rect = target.getBoundingClientRect()
    let left = Math.max(padding, Math.min(rect.right - menuWidth, viewportWidth - menuWidth - padding))
    let top = rect.bottom + 4
    if (top + menuHeight > viewportHeight - padding) {
      top = Math.max(padding, rect.top - menuHeight - 4)
    }
    if (viewportWidth < 768) {
      left = Math.max(padding, Math.min(rect.left + rect.width / 2 - menuWidth / 2, viewportWidth - menuWidth - padding))
    }
    actionMenu.position = { top, left }
  } else {
    actionMenu.position = { top: event.clientY, left: Math.max(padding, event.clientX - menuWidth) }
  }
  actionMenu.show = true
}

function closeDeleteDialog(): void {
  showDeleteDialog.value = false
  accountToDelete.value = null
}

function patchAccountInList(account: Account): void {
  accounts.value = accounts.value.map((item) => (item.id === account.id ? account : item))
}

function closeTestModal(): void {
  showTestModal.value = false
  testingAccount.value = null
}

function closeStatsModal(): void {
  showStatsModal.value = false
  statsAccount.value = null
}

function closeReAuthModal(): void {
  showReAuthModal.value = false
  reAuthAccount.value = null
}

function handleTest(account: Account): void {
  testingAccount.value = account
  showTestModal.value = true
}

function handleViewStats(account: Account): void {
  statsAccount.value = account
  showStatsModal.value = true
}

function handleReAuth(account: Account): void {
  reAuthAccount.value = account
  showReAuthModal.value = true
}

async function handleAccountReauthorized(): Promise<void> {
  showReAuthModal.value = false
  reAuthAccount.value = null
  usageManualRefreshToken.value += 1
  await loadAccounts()
}

async function handleRefreshToken(account: Account): Promise<void> {
  try {
    const result = await accountsAPI.refreshCredentials(account.id)
    patchAccountInList(result.account)
    usageManualRefreshToken.value += 1
    await refreshTodayStatsBatch()
    if (result.warning === 'missing_project_id_temporary') {
      appStore.showWarning(result.message || t('common.warning'))
    } else {
      appStore.showSuccess(t('common.success'))
    }
  } catch (error: any) {
    console.error('Failed to refresh user account token:', error)
    appStore.showError(error?.response?.data?.message || t('admin.accounts.oauth.authFailed'))
  }
}

async function handleSetPrivacy(account: Account): Promise<void> {
  try {
    const updated = await accountsAPI.setPrivacy(account.id)
    patchAccountInList(updated)
    appStore.showSuccess(t('common.success'))
  } catch (error: any) {
    console.error('Failed to set user account privacy:', error)
    appStore.showError(error?.response?.data?.message || t('admin.accounts.privacyFailed'))
  }
}

async function handleVerifyLevel(account: Account, targetLevel: UserAccountVerifyLevelTarget): Promise<void> {
  if (verifyingLevelId.value !== null) {
    appStore.showWarning(t('userAccounts.levelVerifyInProgress'))
    return
  }
  verifyingLevelId.value = account.id
  try {
    appStore.showInfo(
      targetLevel === 'plus'
        ? t('userAccounts.levelVerifyingPlus')
        : t('userAccounts.levelUpdatingFree')
    )
    const result = await accountsAPI.verifyLevel(account.id, targetLevel)
    patchAccountInList(result.account)
    usageManualRefreshToken.value += 1
    await refreshTodayStatsBatch()
    if (targetLevel === 'free') {
      appStore.showSuccess(t('userAccounts.levelFreeUpdated'))
      return
    }
    if (result.verified) {
      appStore.showSuccess(t('userAccounts.levelPlusVerified'))
      return
    }
    const message = result.error_message
      ? t('userAccounts.levelPlusRejectedWithReason', { reason: result.error_message })
      : t('userAccounts.levelPlusRejected')
    appStore.showWarning(message)
  } catch (error: any) {
    console.error('Failed to verify user account level:', error)
    appStore.showError(error?.response?.data?.message || error?.message || t('userAccounts.levelVerifyFailed'))
  } finally {
    verifyingLevelId.value = null
  }
}

async function toggleAccountStatus(account: Account): Promise<void> {
  togglingStatusId.value = account.id
  try {
    const nextStatus: UserAccountStatus = isAccountActive(account) ? 'disabled' : 'active'
    const updated = await accountsAPI.toggleStatus(account.id, nextStatus)
    patchAccountInList(updated)
    await refreshTodayStatsBatch()
    appStore.showSuccess(
      nextStatus === 'active'
        ? t('userAccounts.accountEnabledSuccess')
        : t('userAccounts.accountDisabledSuccess')
    )
  } catch (error) {
    console.error('Failed to toggle user account status:', error)
    appStore.showError(t('userAccounts.failedToUpdateStatus'))
  } finally {
    togglingStatusId.value = null
  }
}

async function toggleSchedulable(account: Account): Promise<void> {
  togglingSchedulableId.value = account.id
  try {
    const updated = await accountsAPI.update(account.id, { schedulable: !account.schedulable })
    patchAccountInList(updated)
    await refreshTodayStatsBatch()
  } catch (error) {
    console.error('Failed to toggle user account schedulable:', error)
    appStore.showError(t('admin.accounts.failedToToggleSchedulable'))
  } finally {
    togglingSchedulableId.value = null
  }
}

async function bulkToggleSchedulable(schedulable: boolean): Promise<void> {
  const accountIds = [...selectedIds.value]
  if (accountIds.length === 0) return

  try {
    const result = await accountsAPI.bulkUpdate(accountIds, { schedulable })
    const successIds = result.success_ids?.length
      ? result.success_ids
      : result.results.filter((item) => item.success).map((item) => item.account_id)
    if (successIds.length > 0) {
      const idSet = new Set(successIds)
      accounts.value = accounts.value.map((account) =>
        idSet.has(account.id) ? { ...account, schedulable } : account
      )
    }
    if (result.failed > 0) {
      appStore.showError(
        t('admin.accounts.bulkSchedulablePartial', {
          success: result.success,
          failed: result.failed
        })
      )
    } else {
      appStore.showSuccess(
        schedulable
          ? t('admin.accounts.bulkSchedulableEnabled', { count: result.success })
          : t('admin.accounts.bulkSchedulableDisabled', { count: result.success })
      )
      clearSelection()
    }
    usageManualRefreshToken.value += 1
    await refreshTodayStatsBatch()
  } catch (error: any) {
    console.error('Failed to bulk toggle user account schedulable:', error)
    appStore.showError(error?.response?.data?.message || t('common.error'))
  }
}

async function bulkRefreshTokens(): Promise<void> {
  const selected = selectedAccounts.value.filter(isRefreshableAccount)
  if (selected.length === 0) {
    appStore.showError(t('admin.accounts.bulkActions.noRefreshableAccounts'))
    return
  }
  try {
    const task = await accountsAPI.createBatchRefreshTask(selected.map(account => account.id))
    appStore.showSuccess(t('admin.accounts.bulkActions.asyncSubmitted', { count: task.total }))
    clearSelection()
    void pollUserAccountBatchTask(task.id, (completed) => {
      if (completed.failed > 0) {
        appStore.showError(t('admin.accounts.bulkActions.partialSuccess', { success: completed.success, failed: completed.failed }))
      } else {
        appStore.showSuccess(t('admin.accounts.bulkActions.refreshTokenSuccess', { count: completed.success }))
      }
    })
  } catch (error: any) {
    console.error('Failed to create user account refresh task:', error)
    appStore.showError(error?.response?.data?.message || error?.message || t('common.error'))
  }
}

async function waitForUserAccountBatchTask(taskId: number): Promise<AccountBatchTask> {
  const deadline = Date.now() + ACCOUNT_BATCH_TASK_POLL_TIMEOUT_MS
  while (!isUnmounted && Date.now() < deadline) {
    const task = await accountsAPI.getBatchTask(taskId)
    if (task.status === 'succeeded' || task.status === 'failed' || task.status === 'canceled') {
      return task
    }
    await new Promise(resolve => setTimeout(resolve, 1500))
  }
  throw new Error(t('admin.accounts.bulkActions.asyncTimeout'))
}

async function pollUserAccountBatchTask(
  taskId: number,
  onCompleted: (task: AccountBatchTask) => void
): Promise<void> {
  if (activeBatchTaskPolls.has(taskId)) return
  activeBatchTaskPolls.add(taskId)
  try {
    const completed = await waitForUserAccountBatchTask(taskId)
    if (isUnmounted) return
    onCompleted(completed)
    usageManualRefreshToken.value += 1
    await loadAccounts()
  } catch (error: any) {
    if (isUnmounted) return
    console.error('Failed to poll user account batch task:', error)
    appStore.showError(error?.response?.data?.message || error?.message || t('common.error'))
  } finally {
    activeBatchTaskPolls.delete(taskId)
  }
}

async function bulkRevalidatePublicShare(): Promise<void> {
  const selected = selectedAccounts.value.filter(canRevalidatePublicShare)
  if (selected.length === 0) {
    appStore.showError(t('userAccounts.noRevalidatableShareAccounts'))
    return
  }
  try {
    const task = await accountsAPI.createBatchRevalidatePublicShareTask(selected.map(account => account.id))
    appStore.showSuccess(t('userAccounts.bulkRevalidateSubmitted', { count: task.total }))
    clearSelection()
    void pollUserAccountBatchTask(task.id, (completed) => {
      if (completed.failed > 0) {
        appStore.showError(t('admin.accounts.bulkActions.partialSuccess', { success: completed.success, failed: completed.failed }))
      } else {
        appStore.showSuccess(t('userAccounts.bulkRevalidateCompleted', { count: completed.success }))
      }
    })
  } catch (error: any) {
    console.error('Failed to create public share revalidation task:', error)
    appStore.showError(error?.response?.data?.message || error?.message || t('userAccounts.shareValidationFailedToRun'))
  }
}

async function revalidatePublicShare(account: Account): Promise<void> {
  revalidatingShareId.value = account.id
  try {
    const updated = await accountsAPI.revalidatePublicShare(account.id)
    patchAccountInList(updated)
    await refreshTodayStatsBatch()
    appStore.showSuccess(
      updated.share_status === 'approved'
        ? t('userAccounts.shareValidationApproved')
        : t('userAccounts.shareValidationStillPending')
    )
  } catch (error: any) {
    console.error('Failed to revalidate public share account:', error)
    appStore.showError(error?.response?.data?.message || t('userAccounts.shareValidationFailedToRun'))
  } finally {
    revalidatingShareId.value = null
  }
}

async function deleteAccount(): Promise<void> {
  if (!accountToDelete.value) return
  try {
    await accountsAPI.delete(accountToDelete.value.id)
    appStore.showSuccess(t('userAccounts.accountDeletedSuccess'))
    closeDeleteDialog()
    clearSelection()
    await loadAccounts()
  } catch (error: any) {
    console.error('Failed to delete user account:', error)
    appStore.showError(error?.response?.data?.message || t('userAccounts.failedToDelete'))
  }
}

async function bulkDeleteAccounts(): Promise<void> {
  const accountIds = [...selectedIds.value]
  if (accountIds.length === 0) {
    closeBulkDeleteDialog()
    return
  }
  try {
    const result = await accountsAPI.bulkDelete(accountIds)
    if (result.success > 0 && result.failed === 0) {
      appStore.showSuccess(t('admin.accounts.bulkDeleteSuccess', { count: result.success }))
    } else if (result.success > 0) {
      appStore.showError(
        t('admin.accounts.bulkDeletePartial', { success: result.success, failed: result.failed })
      )
    } else {
      appStore.showError(t('admin.accounts.bulkDeleteFailed'))
    }
    closeBulkDeleteDialog()
    clearSelection()
    usageManualRefreshToken.value += 1
    await Promise.all([loadGroups(), loadAccounts()])
  } catch (error: any) {
    console.error('Failed to bulk delete user accounts:', error)
    appStore.showError(error?.response?.data?.message || t('admin.accounts.bulkDeleteFailed'))
  }
}

function formatExpiresAt(value: number | null): string {
  if (!value) return '-'
  return formatDateTime(
    new Date(value * 1000),
    {
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      hour12: false
    },
    'sv-SE'
  )
}

function isExpired(value: number | null): boolean {
  if (!value) return false
  return value * 1000 <= Date.now()
}

function handleShowTempUnsched(_account: Account): void {
  appStore.showInfo(t('admin.accounts.status.viewTempUnschedDetails'))
}

onMounted(async () => {
  await authStore.refreshUser().catch((error) => {
    console.error('Failed to refresh shared owner status:', error)
  })
  if (!canManageSharedAccounts.value) {
    return
  }
  await Promise.all([loadGroups(), loadAccounts(), loadRevenuePolicy()])
})

onUnmounted(() => {
  isUnmounted = true
  abortController?.abort()
})
</script>
