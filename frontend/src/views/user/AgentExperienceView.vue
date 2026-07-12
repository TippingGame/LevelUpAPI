<template>
  <AppLayout>
    <div class="agent-experience card flex min-h-[calc(100vh-8rem)] flex-col overflow-hidden xl:h-[calc(100vh-8rem)] xl:min-h-0">
      <header class="flex flex-col gap-4 border-b border-gray-100 px-4 py-4 dark:border-dark-700 sm:px-6">
        <div class="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
          <div class="flex min-w-0 items-center gap-3">
            <div class="flex h-11 w-11 flex-shrink-0 items-center justify-center rounded-2xl bg-gradient-to-br from-primary-500 to-orange-400 text-white shadow-sm">
              <Icon name="sparkles" size="lg" :stroke-width="1.8" />
            </div>
            <div class="min-w-0">
              <div class="flex flex-wrap items-center gap-2">
                <h1 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('agentExperience.title') }}</h1>
                <span class="rounded-full bg-primary-50 px-2 py-0.5 text-[11px] font-medium text-primary-700 dark:bg-primary-900/30 dark:text-primary-300">
                  {{ t('agentExperience.qaMode') }}
                </span>
              </div>
              <p class="mt-0.5 text-sm text-gray-500 dark:text-dark-300">{{ t('agentExperience.subtitle') }}</p>
            </div>
          </div>

          <div class="flex flex-wrap items-center gap-2">
            <a
              :href="PROJECT_URL"
              target="_blank"
              rel="noopener noreferrer"
              class="btn btn-secondary px-3"
            >
              <Icon name="externalLink" size="sm" />
              {{ t('agentExperience.projectHome') }}
            </a>
            <a
              :href="DOWNLOAD_URL"
              target="_blank"
              rel="noopener noreferrer"
              class="btn btn-primary px-3"
            >
              <Icon name="download" size="sm" />
              {{ t('agentExperience.downloadFull') }}
            </a>
          </div>
        </div>

        <div class="grid grid-cols-1 gap-3 md:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto_auto] md:items-end">
          <label class="block min-w-0">
            <span class="mb-1.5 block text-xs font-medium text-gray-600 dark:text-dark-300">{{ t('agentExperience.apiKey') }}</span>
            <Select
              v-model="selectedKeyId"
              :options="keyOptions"
              :placeholder="keysLoading ? t('agentExperience.loadingKeys') : t('agentExperience.selectKey')"
              :empty-text="t('agentExperience.noKeys')"
              :disabled="keysLoading || generating"
              searchable
              @change="handleKeyChange"
            />
          </label>

          <label class="block min-w-0">
            <span class="mb-1.5 block text-xs font-medium text-gray-600 dark:text-dark-300">{{ t('agentExperience.model') }}</span>
            <Select
              v-model="selectedModel"
              :options="modelOptions"
              :placeholder="modelsLoading ? t('agentExperience.loadingModels') : t('agentExperience.selectModel')"
              :empty-text="modelError || t('agentExperience.noModels')"
              :disabled="!selectedKey || modelsLoading || generating"
              searchable
              @change="persistSelection"
            />
          </label>

          <div class="rounded-xl border border-gray-200 bg-gray-50 px-3 py-2.5 text-sm text-gray-700 dark:border-dark-600 dark:bg-dark-800 dark:text-dark-200">
            <span class="mr-1.5 text-xs text-gray-400 dark:text-dark-400">{{ t('agentExperience.mode') }}</span>
            {{ t('agentExperience.qaMode') }}
          </div>

          <button
            type="button"
            class="btn btn-secondary px-3"
            :disabled="messages.length === 0 && !generating"
            :title="t('agentExperience.newChat')"
            @click="clearConversation"
          >
            <Icon name="plus" size="sm" />
            {{ t('agentExperience.newChat') }}
          </button>
        </div>

        <div v-if="keysError || modelError" class="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-red-600 dark:text-red-400">
          <span>{{ keysError || modelError }}</span>
          <button v-if="keysError" type="button" class="font-medium underline underline-offset-2" @click="loadKeys">
            {{ t('common.tryAgain') }}
          </button>
          <button v-else-if="modelError" type="button" class="font-medium underline underline-offset-2" @click="() => loadModels()">
            {{ t('common.tryAgain') }}
          </button>
        </div>
      </header>

      <main ref="messagePane" class="min-h-0 flex-1 overflow-y-auto px-4 py-6 sm:px-6">
        <div v-if="messages.length === 0" class="mx-auto flex h-full max-w-2xl flex-col items-center justify-center py-10 text-center">
          <div class="flex h-14 w-14 items-center justify-center rounded-2xl bg-primary-50 text-primary-600 dark:bg-primary-900/25 dark:text-primary-300">
            <Icon name="chat" size="xl" />
          </div>
          <h2 class="mt-5 text-xl font-semibold text-gray-900 dark:text-white">{{ emptyStateTitle }}</h2>
          <p class="mt-2 max-w-xl text-sm leading-6 text-gray-500 dark:text-dark-300">{{ emptyStateDescription }}</p>

          <RouterLink
            v-if="!keysLoading && apiKeys.length === 0"
            to="/keys"
            class="btn btn-primary mt-5"
          >
            <Icon name="key" size="sm" />
            {{ t('agentExperience.createKey') }}
          </RouterLink>

          <div v-else-if="selectedKey && selectedModel" class="mt-6 flex max-w-2xl flex-wrap justify-center gap-2">
            <button
              v-for="prompt in suggestedPrompts"
              :key="prompt"
              type="button"
              class="rounded-full border border-gray-200 bg-white px-3 py-2 text-sm text-gray-600 transition-colors hover:border-primary-300 hover:text-primary-600 dark:border-dark-600 dark:bg-dark-800 dark:text-dark-200 dark:hover:border-primary-700 dark:hover:text-primary-300"
              @click="useSuggestedPrompt(prompt)"
            >
              {{ prompt }}
            </button>
          </div>

        </div>

        <div v-else class="mx-auto max-w-4xl space-y-6" aria-live="polite">
          <article
            v-for="message in messages"
            :key="message.id"
            class="flex gap-3"
            :class="message.role === 'user' ? 'justify-end' : 'justify-start'"
          >
            <div v-if="message.role === 'assistant'" class="mt-0.5 flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-xl bg-gradient-to-br from-primary-500 to-orange-400 text-white">
              <Icon name="sparkles" size="sm" />
            </div>

            <div class="min-w-0" :class="message.role === 'user' ? 'max-w-[85%] sm:max-w-[75%]' : 'max-w-[calc(100%-2.75rem)] flex-1'">
              <div
                class="message-bubble"
                :class="[
                  message.role === 'user' ? 'message-bubble-user' : 'message-bubble-assistant',
                  message.error && 'message-bubble-error',
                ]"
              >
                <div v-if="message.role === 'assistant' && message.content" class="markdown-body" v-html="renderMarkdown(message.content)"></div>
                <p v-else-if="message.content" class="whitespace-pre-wrap break-words">{{ message.content }}</p>
                <span v-else-if="message.streaming" class="typing-indicator" :aria-label="t('agentExperience.generating')">
                  <i></i><i></i><i></i>
                </span>
              </div>
              <div v-if="message.role === 'assistant' && message.content && !message.streaming && !message.error" class="mt-1.5 flex items-center gap-2">
                <span class="text-[11px] text-gray-400 dark:text-dark-400">{{ message.model || selectedModel }}</span>
                <button type="button" class="text-gray-400 hover:text-gray-700 dark:text-dark-400 dark:hover:text-dark-100" :title="t('common.copy')" @click="copyMessage(message.content)">
                  <Icon name="copy" size="xs" />
                </button>
              </div>
            </div>
          </article>
        </div>
      </main>

      <footer class="border-t border-gray-100 bg-white/80 px-4 py-4 backdrop-blur dark:border-dark-700 dark:bg-dark-900/80 sm:px-6">
        <div class="mx-auto max-w-4xl">
          <div class="relative rounded-2xl border border-gray-200 bg-white shadow-sm transition-colors focus-within:border-primary-400 focus-within:ring-2 focus-within:ring-primary-500/10 dark:border-dark-600 dark:bg-dark-800">
            <textarea
              ref="composer"
              v-model="draft"
              rows="1"
              :placeholder="composerPlaceholder"
              :disabled="!canCompose"
              class="max-h-40 min-h-[3.25rem] w-full resize-none bg-transparent px-4 py-3.5 pr-14 text-sm text-gray-900 outline-none placeholder:text-gray-400 disabled:cursor-not-allowed disabled:opacity-60 dark:text-white dark:placeholder:text-dark-400"
              @input="resizeComposer"
              @keydown="handleComposerKeydown"
            ></textarea>
            <button
              v-if="generating"
              type="button"
              class="absolute bottom-2.5 right-2.5 flex h-8 w-8 items-center justify-center rounded-xl bg-gray-900 text-white transition-colors hover:bg-gray-700 dark:bg-gray-100 dark:text-gray-900 dark:hover:bg-white"
              :title="t('agentExperience.stop')"
              @click="stopGeneration"
            >
              <span class="h-2.5 w-2.5 rounded-sm bg-current"></span>
            </button>
            <button
              v-else
              type="button"
              class="absolute bottom-2.5 right-2.5 flex h-8 w-8 items-center justify-center rounded-xl bg-primary-500 text-white transition-colors hover:bg-primary-600 disabled:cursor-not-allowed disabled:opacity-40"
              :disabled="!canSend"
              :title="t('agentExperience.send')"
              @click="sendMessage"
            >
              <Icon name="arrowUp" size="sm" :stroke-width="2" />
            </button>
          </div>
          <p class="mt-2 text-center text-[11px] text-gray-400 dark:text-dark-400">{{ t('agentExperience.inputHint') }}</p>
        </div>
      </footer>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import DOMPurify from 'dompurify'
import { marked } from 'marked'
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { fetchAgentExperienceModels, streamAgentExperienceChat } from '@/api/agentExperience'
import type { AgentExperienceMessage } from '@/api/agentExperience'
import { keysAPI } from '@/api/keys'
import AppLayout from '@/components/layout/AppLayout.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import type { ApiKey } from '@/types'
import { extractApiErrorMessage } from '@/utils/apiError'

interface DisplayMessage extends AgentExperienceMessage {
  id: string
  streaming?: boolean
  error?: boolean
  model?: string
}

interface SavedSelection {
  keyId?: number
  model?: string
}

const PROJECT_URL = 'https://github.com/TippingGame/LevelUpAgent'
const DOWNLOAD_URL = 'https://github.com/TippingGame/LevelUpAgent/releases'
const SELECTION_STORAGE_KEY = 'levelup_agent_experience_selection'
const CHAT_STORAGE_KEY = 'levelup_agent_experience_chat'

const { t } = useI18n()
const appStore = useAppStore()

const apiKeys = ref<ApiKey[]>([])
const models = ref<string[]>([])
const selectedKeyId = ref<number | null>(null)
const selectedModel = ref<string | null>(null)
const keysLoading = ref(false)
const modelsLoading = ref(false)
const keysError = ref('')
const modelError = ref('')
const messages = ref<DisplayMessage[]>(restoreConversation())
const draft = ref('')
const generating = ref(false)
const messagePane = ref<HTMLElement | null>(null)
const composer = ref<HTMLTextAreaElement | null>(null)
let modelRequestId = 0
let modelAbortController: AbortController | null = null
let chatAbortController: AbortController | null = null

const selectedKey = computed(() => apiKeys.value.find((key) => key.id === selectedKeyId.value) ?? null)
const keyOptions = computed(() => apiKeys.value.map((key) => ({
  value: key.id,
  label: formatKeyLabel(key),
})))
const modelOptions = computed(() => models.value.map((model) => ({ value: model, label: model })))
const canCompose = computed(() => Boolean(selectedKey.value && selectedModel.value) && !keysLoading.value && !modelsLoading.value)
const canSend = computed(() => canCompose.value && !generating.value && Boolean(draft.value.trim()))
const suggestedPrompts = computed(() => [
  t('agentExperience.promptExplain'),
  t('agentExperience.promptPlan'),
  t('agentExperience.promptWrite'),
])
const emptyStateTitle = computed(() => {
  if (keysLoading.value) return t('agentExperience.preparing')
  if (apiKeys.value.length === 0) return t('agentExperience.noKeyTitle')
  if (modelsLoading.value) return t('agentExperience.loadingModels')
  if (!selectedModel.value) return t('agentExperience.chooseModelTitle')
  return t('agentExperience.readyTitle')
})
const emptyStateDescription = computed(() => {
  if (apiKeys.value.length === 0 && !keysLoading.value) return t('agentExperience.noKeyDescription')
  if (!selectedModel.value) return t('agentExperience.chooseModelDescription')
  return t('agentExperience.readyDescription')
})
const composerPlaceholder = computed(() => {
  if (keysLoading.value) return t('agentExperience.loadingKeys')
  if (!selectedKey.value) return t('agentExperience.selectKeyFirst')
  if (modelsLoading.value) return t('agentExperience.loadingModels')
  if (!selectedModel.value) return t('agentExperience.selectModelFirst')
  return t('agentExperience.inputPlaceholder')
})

function generateMessageId(): string {
  return `${Date.now()}-${Math.random().toString(36).slice(2, 9)}`
}

function restoreSelection(): SavedSelection {
  try {
    return JSON.parse(localStorage.getItem(SELECTION_STORAGE_KEY) || '{}') as SavedSelection
  } catch {
    return {}
  }
}

function restoreConversation(): DisplayMessage[] {
  try {
    const saved = JSON.parse(sessionStorage.getItem(CHAT_STORAGE_KEY) || '[]') as Array<Partial<DisplayMessage>>
    if (!Array.isArray(saved)) return []
    return saved
      .filter((message) => (message.role === 'user' || message.role === 'assistant') && typeof message.content === 'string' && message.content)
      .slice(-50)
      .map((message) => ({
        id: typeof message.id === 'string' ? message.id : generateMessageId(),
        role: message.role as 'user' | 'assistant',
        content: message.content as string,
        error: Boolean(message.error),
        model: typeof message.model === 'string' ? message.model : undefined,
      }))
  } catch {
    return []
  }
}

function persistSelection(): void {
  try {
    localStorage.setItem(SELECTION_STORAGE_KEY, JSON.stringify({
      keyId: selectedKeyId.value ?? undefined,
      model: selectedModel.value ?? undefined,
    }))
  } catch {
    // Storage can be unavailable in privacy modes; the page remains fully usable.
  }
}

function persistConversation(): void {
  try {
    const stableMessages = messages.value
      .filter((message) => message.content)
      .slice(-50)
      .map(({ id, role, content, error, model }) => ({ id, role, content, error, model }))
    sessionStorage.setItem(CHAT_STORAGE_KEY, JSON.stringify(stableMessages))
  } catch {
    // Storage can be unavailable in privacy modes; the conversation still works in memory.
  }
}

function formatKeyLabel(key: ApiKey): string {
  const suffix = key.key ? key.key.slice(-4) : '----'
  const groupName = key.group?.name ? ` · ${key.group.name}` : ''
  return `${key.name}${groupName} · ••••${suffix}`
}

async function loadKeys(): Promise<void> {
  keysLoading.value = true
  keysError.value = ''
  try {
    const result = await keysAPI.list(1, 1000, { status: 'active', sort_by: 'created_at', sort_order: 'desc' })
    apiKeys.value = result.items.filter((key) => key.status === 'active')
    const saved = restoreSelection()
    const preferred = apiKeys.value.find((key) => key.id === saved.keyId) ?? apiKeys.value[0] ?? null
    selectedKeyId.value = preferred?.id ?? null
    if (preferred) await loadModels(saved.model)
  } catch (error) {
    keysError.value = extractApiErrorMessage(error, t('agentExperience.loadKeysFailed'))
  } finally {
    keysLoading.value = false
  }
}

async function loadModels(preferredModel?: string): Promise<void> {
  const key = selectedKey.value
  modelAbortController?.abort()
  const requestId = ++modelRequestId
  models.value = []
  selectedModel.value = null
  modelError.value = ''
  if (!key) return

  modelsLoading.value = true
  modelAbortController = new AbortController()
  try {
    const result = await fetchAgentExperienceModels(key.key, modelAbortController.signal)
    if (requestId !== modelRequestId) return
    models.value = result.map((model) => model.id)
    selectedModel.value = preferredModel && models.value.includes(preferredModel)
      ? preferredModel
      : models.value[0] ?? null
    persistSelection()
  } catch (error) {
    if (error instanceof DOMException && error.name === 'AbortError') return
    if (requestId !== modelRequestId) return
    modelError.value = error instanceof Error ? error.message : t('agentExperience.loadModelsFailed')
  } finally {
    if (requestId === modelRequestId) modelsLoading.value = false
  }
}

function handleKeyChange(): void {
  persistSelection()
  void loadModels()
}

function renderMarkdown(content: string): string {
  const html = marked.parse(content, { async: false, breaks: true, gfm: true }) as string
  return DOMPurify.sanitize(html)
}

async function scrollToBottom(): Promise<void> {
  await nextTick()
  if (messagePane.value) messagePane.value.scrollTop = messagePane.value.scrollHeight
}

function resizeComposer(): void {
  if (!composer.value) return
  composer.value.style.height = 'auto'
  composer.value.style.height = `${Math.min(composer.value.scrollHeight, 160)}px`
}

function useSuggestedPrompt(prompt: string): void {
  draft.value = prompt
  nextTick(() => {
    resizeComposer()
    composer.value?.focus()
  })
}

function handleComposerKeydown(event: KeyboardEvent): void {
  if (event.key !== 'Enter' || event.shiftKey || event.isComposing) return
  event.preventDefault()
  void sendMessage()
}

async function sendMessage(): Promise<void> {
  const content = draft.value.trim()
  const key = selectedKey.value
  const model = selectedModel.value
  if (!content || !key || !model || generating.value) return

  const userMessage: DisplayMessage = { id: generateMessageId(), role: 'user', content }
  const assistantMessage: DisplayMessage = { id: generateMessageId(), role: 'assistant', content: '', streaming: true, model }
  messages.value.push(userMessage, assistantMessage)
  persistConversation()
  draft.value = ''
  generating.value = true
  resizeComposer()
  await scrollToBottom()

  const requestMessages = messages.value
    .filter((message) => message.id !== assistantMessage.id && !message.error && message.content)
    .map(({ role, content: messageContent }) => ({ role, content: messageContent }))

  chatAbortController = new AbortController()
  try {
    await streamAgentExperienceChat({
      apiKey: key.key,
      model,
      messages: requestMessages,
      signal: chatAbortController.signal,
      onDelta: (delta) => {
        assistantMessage.content += delta
        void scrollToBottom()
      },
    })
    if (!assistantMessage.content.trim()) {
      assistantMessage.content = t('agentExperience.emptyResponse')
      assistantMessage.error = true
    }
  } catch (error) {
    if (error instanceof DOMException && error.name === 'AbortError') {
      if (!assistantMessage.content) messages.value = messages.value.filter((message) => message.id !== assistantMessage.id)
    } else {
      assistantMessage.content = error instanceof Error ? error.message : t('agentExperience.requestFailed')
      assistantMessage.error = true
    }
  } finally {
    assistantMessage.streaming = false
    generating.value = false
    chatAbortController = null
    persistConversation()
    await scrollToBottom()
    composer.value?.focus()
  }
}

function stopGeneration(): void {
  chatAbortController?.abort()
}

function clearConversation(): void {
  stopGeneration()
  messages.value = []
  draft.value = ''
  try {
    sessionStorage.removeItem(CHAT_STORAGE_KEY)
  } catch {
    // Ignore unavailable storage.
  }
  nextTick(() => {
    resizeComposer()
    composer.value?.focus()
  })
}

async function copyMessage(content: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(content)
    appStore.showSuccess(t('common.copied'))
  } catch {
    appStore.showError(t('common.copyFailed'))
  }
}

onMounted(() => {
  void loadKeys()
  void scrollToBottom()
})

onBeforeUnmount(() => {
  modelAbortController?.abort()
  chatAbortController?.abort()
})
</script>

<style scoped>
.message-bubble {
  @apply rounded-2xl px-4 py-3 text-sm leading-6;
}

.message-bubble-user {
  @apply rounded-br-md bg-primary-500 text-white shadow-sm;
}

.message-bubble-assistant {
  @apply rounded-tl-md border border-gray-100 bg-gray-50/80 text-gray-800 dark:border-dark-700 dark:bg-dark-800/80 dark:text-dark-100;
}

.message-bubble-error {
  @apply border-red-200 bg-red-50 text-red-700 dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-300;
}

.typing-indicator {
  @apply inline-flex h-6 items-center gap-1;
}

.typing-indicator i {
  @apply h-1.5 w-1.5 rounded-full bg-gray-400 dark:bg-dark-300;
  animation: agent-typing 1.2s infinite ease-in-out;
}

.typing-indicator i:nth-child(2) { animation-delay: 0.15s; }
.typing-indicator i:nth-child(3) { animation-delay: 0.3s; }

.markdown-body :deep(p) { @apply mb-3 last:mb-0; }
.markdown-body :deep(h1) { @apply mb-3 mt-5 text-xl font-semibold first:mt-0; }
.markdown-body :deep(h2) { @apply mb-3 mt-5 text-lg font-semibold first:mt-0; }
.markdown-body :deep(h3) { @apply mb-2 mt-4 text-base font-semibold first:mt-0; }
.markdown-body :deep(ul) { @apply mb-3 list-disc space-y-1 pl-5; }
.markdown-body :deep(ol) { @apply mb-3 list-decimal space-y-1 pl-5; }
.markdown-body :deep(blockquote) { @apply my-3 border-l-2 border-primary-300 pl-3 text-gray-500 dark:text-dark-300; }
.markdown-body :deep(a) { @apply text-primary-600 underline underline-offset-2 dark:text-primary-400; }
.markdown-body :deep(code) { @apply rounded bg-gray-200/70 px-1.5 py-0.5 font-mono text-[0.85em] dark:bg-dark-700; }
.markdown-body :deep(pre) { @apply my-3 overflow-x-auto rounded-xl bg-gray-900 p-4 text-gray-100; }
.markdown-body :deep(pre code) { @apply bg-transparent p-0 text-xs; }
.markdown-body :deep(table) { @apply my-3 w-full border-collapse text-left text-sm; }
.markdown-body :deep(th) { @apply border border-gray-200 bg-gray-100 px-3 py-2 dark:border-dark-600 dark:bg-dark-700; }
.markdown-body :deep(td) { @apply border border-gray-200 px-3 py-2 dark:border-dark-600; }
.markdown-body :deep(hr) { @apply my-4 border-gray-200 dark:border-dark-600; }

@keyframes agent-typing {
  0%, 60%, 100% { transform: translateY(0); opacity: 0.45; }
  30% { transform: translateY(-3px); opacity: 1; }
}
</style>
