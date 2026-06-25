<template>
  <!-- Custom Home Content: Full Page Mode -->
  <div v-if="homeContent" class="min-h-screen">
    <!-- iframe mode -->
    <iframe
      v-if="isHomeContentUrl"
      :src="homeContent.trim()"
      class="h-screen w-full border-0"
      allowfullscreen
    ></iframe>
    <!-- HTML mode - SECURITY: homeContent is admin-only setting, XSS risk is acceptable -->
    <div v-else v-html="homeContent"></div>
  </div>

  <!-- Default Home Page -->
  <div v-else class="home-page">
    <header class="home-shell home-nav">
      <router-link class="brand" to="/home" aria-label="Home">
        <span class="brand-mark" aria-hidden="true">
          <img :src="siteLogo || '/logo.svg'" alt="" />
        </span>
        <span>{{ siteName }}</span>
      </router-link>

      <nav class="home-nav-links" aria-label="Home navigation">
        <router-link to="/home">{{ t('home.nav.home') }}</router-link>
        <router-link to="/key-usage">{{ t('home.nav.usage') }}</router-link>
      </nav>

      <div class="home-actions">
        <a
          v-if="docUrl"
          :href="docUrl"
          target="_blank"
          rel="noopener noreferrer"
          class="doc-action"
          :title="t('home.viewDocs')"
        >
          <Icon name="book" size="sm" />
          <span>{{ t('home.docs') }}</span>
        </a>

        <LocaleSwitcher />

        <button
          type="button"
          class="icon-action"
          :title="isDark ? t('home.switchToLight') : t('home.switchToDark')"
          @click="toggleTheme"
        >
          <Icon v-if="isDark" name="sun" size="md" />
          <Icon v-else name="moon" size="md" />
        </button>

        <router-link
          v-if="isAuthenticated"
          :to="dashboardPath"
          class="button primary nav-cta"
        >
          <span class="user-dot">{{ userInitial }}</span>
          <span>{{ t('home.dashboard') }}</span>
        </router-link>
        <router-link v-else to="/login" class="button primary nav-cta">
          {{ t('home.login') }}
        </router-link>
      </div>
    </header>

    <main class="home-shell hero">
      <section class="copy">
        <span class="eyebrow">
          <svg viewBox="0 0 24 24" fill="none" aria-hidden="true">
            <circle cx="12" cy="12" r="5" fill="currentColor" />
            <path
              d="M12 2v4m0 12v4M2 12h4m12 0h4"
              stroke="currentColor"
              stroke-width="2"
              stroke-linecap="round"
            />
          </svg>
          {{ t('home.arcade.eyebrow') }}
        </span>

        <h1 class="home-title">
          {{ t('home.arcade.titlePrefix') }}<span class="flow-text">{{ t('home.arcade.titleHighlight') }}</span>
        </h1>

        <p class="lead">
          {{ t('home.arcade.lead', { siteName }) }}
        </p>

        <div class="hero-actions">
          <router-link :to="isAuthenticated ? dashboardPath : '/login'" class="button primary">
            {{ isAuthenticated ? t('home.goToDashboard') : t('home.getStarted') }}
          </router-link>
          <router-link class="button secondary" to="/key-usage">{{ t('home.arcade.viewUsage') }}</router-link>
        </div>
      </section>

      <aside class="visual" :aria-label="t('home.arcade.visualLabel')">
        <div class="pixel-decor pixel-decor-one" aria-hidden="true"></div>
        <div class="pixel-decor pixel-decor-two" aria-hidden="true"></div>
        <div class="pixel-decor pixel-decor-three" aria-hidden="true"></div>

        <div class="console-card">
          <div class="console-inner">
            <div class="side-rail">
              <div class="rail-logo">
                <img :src="siteLogo || '/logo.svg'" alt="" />
              </div>
              <i class="active">
                <svg fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    d="M3 12l2-2m0 0l7-7 7 7M5 10v10a1 1 0 001 1h3m10-11l2 2m-2-2v10a1 1 0 01-1 1h-3m-6 0a1 1 0 001-1v-4a1 1 0 011-1h2a1 1 0 011 1v4a1 1 0 001 1m-6 0h6"
                  ></path>
                </svg>
              </i>
              <i>
                <svg fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    d="M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z"
                  ></path>
                </svg>
              </i>
              <i>
                <svg fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z"
                  ></path>
                </svg>
              </i>
              <i>
                <svg fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    d="M20 7l-8-4-8 4m16 0l-8 4m8-4v10l-8 4m0-10L4 7m8 4v10M4 7v10l8 4"
                  ></path>
                </svg>
              </i>
            </div>

            <div class="dashboard-board">
              <div
                ref="gameStageRef"
                class="token-arcade"
                tabindex="0"
                role="application"
                :aria-label="t('home.arcade.stageLabel')"
                @pointerdown="handleStagePointer"
                @pointermove="handleStagePointer"
                @keydown="handleGameKeydown"
                @keyup="handleGameKeyup"
              >
                <div class="arcade-bezel">
                  <div class="arcade-topline">
                    <span>{{ t('home.arcade.level') }} {{ gameWaveText }}</span>
                    <span>{{ t('home.arcade.score') }} {{ gameScoreText }}</span>
                    <span>{{ t('home.arcade.core') }} {{ gameEnergyText }}</span>
                  </div>

                  <div class="arcade-screen">
                    <div class="pixel-stars" aria-hidden="true"></div>
                    <div class="city-line" aria-hidden="true">
                      <span v-for="tower in cityTowers" :key="tower.id" :style="{ height: tower.height }"></span>
                    </div>

                    <div class="hero-sprite" :style="heroStyle" aria-hidden="true">
                      <span class="hero-head"></span>
                      <span class="hero-core"></span>
                      <span class="hero-body"></span>
                      <span class="hero-arm hero-arm-left"></span>
                      <span class="hero-arm hero-arm-right"></span>
                      <span class="hero-leg hero-leg-left"></span>
                      <span class="hero-leg hero-leg-right"></span>
                    </div>

                    <span
                      v-for="beam in gameBeams"
                      :key="beam.id"
                      class="light-beam"
                      :style="beamStyle(beam)"
                      aria-hidden="true"
                    ></span>

                    <span
                      v-for="enemy in gameEnemies"
                      :key="enemy.id"
                      class="token-enemy"
                      :style="enemyStyle(enemy)"
                      aria-hidden="true"
                    >
                      {{ enemy.label }}
                    </span>

                    <span
                      v-for="burst in gameBursts"
                      :key="burst.id"
                      class="token-burst"
                      :style="burstStyle(burst)"
                      aria-hidden="true"
                    ></span>
                  </div>

                  <div class="arcade-panel">
                    <div class="power-meter" aria-hidden="true">
                      <span :style="{ width: `${gameEnergy}%` }"></span>
                    </div>
                    <div class="arcade-controls" aria-hidden="true">
                      <button type="button" :title="t('home.arcade.controls.up')" @pointerdown.stop.prevent="nudgeHero(0, -8)">▲</button>
                      <button type="button" :title="t('home.arcade.controls.left')" @pointerdown.stop.prevent="nudgeHero(-8, 0)">◀</button>
                      <button type="button" :title="t('home.arcade.controls.down')" @pointerdown.stop.prevent="nudgeHero(0, 8)">▼</button>
                      <button type="button" :title="t('home.arcade.controls.right')" @pointerdown.stop.prevent="nudgeHero(8, 0)">▶</button>
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </div>
      </aside>
    </main>

    <section class="home-shell step-strip" :aria-label="t('home.arcade.stepsLabel')">
      <h2><span class="flow-text">3</span>{{ t('home.arcade.stepsTitleAfterSteps') }}<span class="flow-text">2</span>{{ t('home.arcade.stepsTitleAfterMinutes') }}</h2>

      <div class="steps">
        <article class="step-card">
          <span class="step-num">1</span>
          <span class="step-icon">
            <svg viewBox="0 0 24 24" fill="none">
              <path d="M15 19a6 6 0 0 0-12 0" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" />
              <path d="M9 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8Z" stroke="currentColor" stroke-width="2.2" />
              <path d="M19 8v6M16 11h6" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" />
            </svg>
          </span>
          <div>
            <h3>{{ t('home.arcade.steps.register.title') }}</h3>
            <p>{{ t('home.arcade.steps.register.desc') }}</p>
          </div>
        </article>

        <span class="step-arrow" aria-hidden="true"></span>

        <article class="step-card">
          <span class="step-num">2</span>
          <span class="step-icon">
            <svg viewBox="0 0 24 24" fill="none">
              <path d="M21 7a5 5 0 0 1-6.8 4.7L6.5 19.4 3 21l1.6-3.5 7.7-7.7A5 5 0 1 1 21 7Z" stroke="currentColor" stroke-width="2.2" stroke-linejoin="round" />
              <path d="M17.5 6.5h.01" stroke="currentColor" stroke-width="3.5" stroke-linecap="round" />
            </svg>
          </span>
          <div>
            <h3>{{ t('home.arcade.steps.apiKey.title') }}</h3>
            <p>{{ t('home.arcade.steps.apiKey.desc') }}</p>
          </div>
        </article>

        <span class="step-arrow" aria-hidden="true"></span>

        <article class="step-card">
          <span class="step-num">3</span>
          <span class="step-icon">
            <svg viewBox="0 0 24 24" fill="none">
              <path d="m8 16-4-4 4-4M16 8l4 4-4 4" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" />
            </svg>
          </span>
          <div>
            <h3>{{ t('home.arcade.steps.baseUrl.title') }}</h3>
            <p>{{ t('home.arcade.steps.baseUrl.desc') }}</p>
          </div>
        </article>
      </div>
    </section>

    <footer class="home-shell home-footer">
      <span>&copy; {{ currentYear }} {{ siteName }}. {{ t('home.footer.allRightsReserved') }}</span>
    </footer>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore, useAppStore } from '@/stores'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import Icon from '@/components/icons/Icon.vue'

const { t, locale } = useI18n()

const authStore = useAuthStore()
const appStore = useAppStore()

// Site settings - directly from appStore (already initialized from injected config)
const siteName = computed(() => appStore.cachedPublicSettings?.site_name || appStore.siteName || 'LevelUpAPI')
const siteLogo = computed(() => appStore.cachedPublicSettings?.site_logo || appStore.siteLogo || '')
const docUrl = computed(() => appStore.cachedPublicSettings?.doc_url || appStore.docUrl || '')
const homeContent = computed(() => appStore.cachedPublicSettings?.home_content || '')

// Check if homeContent is a URL (for iframe display)
const isHomeContentUrl = computed(() => {
  const content = homeContent.value.trim()
  return content.startsWith('http://') || content.startsWith('https://')
})

// Theme
const isDark = ref(document.documentElement.classList.contains('dark'))

// Auth state
const isAuthenticated = computed(() => authStore.isAuthenticated)
const isAdmin = computed(() => authStore.isAdmin)
const dashboardPath = computed(() => isAdmin.value ? '/admin/dashboard' : '/dashboard')
const userInitial = computed(() => {
  const user = authStore.user
  if (!user || !user.email) return ''
  return user.email.charAt(0).toUpperCase()
})

// Current year for footer
const currentYear = computed(() => new Date().getFullYear())

interface TokenEnemy {
  id: number
  x: number
  y: number
  speed: number
  label: string
  wobble: number
}

interface LightBeam {
  id: number
  x: number
  y: number
  speed: number
}

interface TokenBurst {
  id: number
  x: number
  y: number
  age: number
}

const gameStageRef = ref<HTMLElement | null>(null)
const heroPosition = ref({ x: 16, y: 50 })
const gameEnemies = ref<TokenEnemy[]>([])
const gameBeams = ref<LightBeam[]>([])
const gameBursts = ref<TokenBurst[]>([])
const gameScore = ref(0)
const gameWave = ref(1)
const gameEnergy = ref(100)
const pressedKeys = new Set<string>()
let gameFrameId = 0
let lastFrameAt = 0
let lastShotAt = 0
let lastEnemyAt = 0
let gameIdSeed = 1

const cityTowers = [
  { id: 1, height: '18%' },
  { id: 2, height: '31%' },
  { id: 3, height: '24%' },
  { id: 4, height: '38%' },
  { id: 5, height: '21%' },
  { id: 6, height: '34%' },
  { id: 7, height: '26%' },
  { id: 8, height: '30%' },
]

const gameScoreText = computed(() => formatInteger(gameScore.value))
const gameWaveText = computed(() => String(gameWave.value).padStart(2, '0'))
const gameEnergyText = computed(() => `${Math.round(gameEnergy.value)}%`)
const heroStyle = computed(() => ({
  left: `${heroPosition.value.x}%`,
  top: `${heroPosition.value.y}%`,
}))

function formatInteger(value: number): string {
  if (!Number.isFinite(value)) return '--'
  const numberLocale = locale.value === 'zh' ? 'zh-CN' : 'en-US'
  return new Intl.NumberFormat(numberLocale, {
    maximumFractionDigits: 0
  }).format(value)
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value))
}

function nudgeHero(dx: number, dy: number) {
  heroPosition.value = {
    x: clamp(heroPosition.value.x + dx, 8, 38),
    y: clamp(heroPosition.value.y + dy, 18, 82),
  }
  gameStageRef.value?.focus()
}

function handleStagePointer(event: PointerEvent) {
  const stage = gameStageRef.value
  if (!stage) return
  const screen = stage.querySelector('.arcade-screen')
  if (!(screen instanceof HTMLElement)) return

  const rect = screen.getBoundingClientRect()
  if (rect.width <= 0 || rect.height <= 0) return

  heroPosition.value = {
    x: clamp(((event.clientX - rect.left) / rect.width) * 100, 8, 38),
    y: clamp(((event.clientY - rect.top) / rect.height) * 100, 18, 82),
  }
  stage.focus()
}

function handleGameKeydown(event: KeyboardEvent) {
  const key = event.key.toLowerCase()
  if (!['arrowup', 'arrowdown', 'arrowleft', 'arrowright', 'w', 'a', 's', 'd'].includes(key)) return
  event.preventDefault()
  pressedKeys.add(key)
}

function handleGameKeyup(event: KeyboardEvent) {
  pressedKeys.delete(event.key.toLowerCase())
}

function beamStyle(beam: LightBeam) {
  return {
    left: `${beam.x}%`,
    top: `${beam.y}%`,
  }
}

function enemyStyle(enemy: TokenEnemy) {
  return {
    left: `${enemy.x}%`,
    top: `${enemy.y}%`,
    '--wobble': `${enemy.wobble}s`,
  }
}

function burstStyle(burst: TokenBurst) {
  return {
    left: `${burst.x}%`,
    top: `${burst.y}%`,
    opacity: String(clamp(1 - burst.age / 520, 0, 1)),
  }
}

function spawnEnemy(now: number) {
  const labels = ['TOK', 'API', '429', 'ERR', 'TTL']
  const interval = Math.max(620, 1300 - gameWave.value * 72)
  if (now - lastEnemyAt < interval) return

  lastEnemyAt = now
  gameEnemies.value.push({
    id: gameIdSeed++,
    x: 104,
    y: 18 + Math.random() * 64,
    speed: 9 + Math.random() * 6 + gameWave.value * 0.8,
    label: labels[Math.floor(Math.random() * labels.length)],
    wobble: 1.4 + Math.random() * 1.2,
  })
}

function fireBeam(now: number) {
  if (now - lastShotAt < 360) return
  lastShotAt = now
  gameBeams.value.push({
    id: gameIdSeed++,
    x: heroPosition.value.x + 7,
    y: heroPosition.value.y - 1,
    speed: 62,
  })
}

function updateHeroFromKeys(delta: number) {
  const step = 46 * delta
  let dx = 0
  let dy = 0
  if (pressedKeys.has('arrowleft') || pressedKeys.has('a')) dx -= step
  if (pressedKeys.has('arrowright') || pressedKeys.has('d')) dx += step
  if (pressedKeys.has('arrowup') || pressedKeys.has('w')) dy -= step
  if (pressedKeys.has('arrowdown') || pressedKeys.has('s')) dy += step
  if (dx || dy) nudgeHero(dx, dy)
}

function resolveHits() {
  const hitEnemyIds = new Set<number>()
  const hitBeamIds = new Set<number>()
  const bursts: TokenBurst[] = []

  for (const beam of gameBeams.value) {
    for (const enemy of gameEnemies.value) {
      if (hitEnemyIds.has(enemy.id)) continue
      const closeX = Math.abs(beam.x - enemy.x) < 5.8
      const closeY = Math.abs(beam.y - enemy.y) < 8.5
      if (!closeX || !closeY) continue
      hitEnemyIds.add(enemy.id)
      hitBeamIds.add(beam.id)
      bursts.push({ id: gameIdSeed++, x: enemy.x, y: enemy.y, age: 0 })
      gameScore.value += 100
      if (gameScore.value > 0 && gameScore.value % 700 === 0) {
        gameWave.value += 1
      }
      break
    }
  }

  if (hitEnemyIds.size > 0) {
    gameEnemies.value = gameEnemies.value.filter((enemy) => !hitEnemyIds.has(enemy.id))
    gameBeams.value = gameBeams.value.filter((beam) => !hitBeamIds.has(beam.id))
    gameBursts.value.push(...bursts)
    gameEnergy.value = clamp(gameEnergy.value + hitEnemyIds.size * 3, 0, 100)
  }
}

function updateGame(now: number) {
  const delta = Math.min((now - lastFrameAt) / 1000 || 0.016, 0.04)
  lastFrameAt = now

  updateHeroFromKeys(delta)
  spawnEnemy(now)
  fireBeam(now)

  gameBeams.value = gameBeams.value
    .map((beam) => ({ ...beam, x: beam.x + beam.speed * delta }))
    .filter((beam) => beam.x < 108)

  gameEnemies.value = gameEnemies.value
    .map((enemy) => ({ ...enemy, x: enemy.x - enemy.speed * delta }))
    .filter((enemy) => {
      const escaped = enemy.x < -8
      if (escaped) gameEnergy.value = clamp(gameEnergy.value - 7, 0, 100)
      return !escaped
    })

  gameBursts.value = gameBursts.value
    .map((burst) => ({ ...burst, age: burst.age + delta * 1000 }))
    .filter((burst) => burst.age < 520)

  if (gameEnergy.value <= 0) {
    gameEnergy.value = 100
    gameWave.value = 1
    gameScore.value = Math.max(0, gameScore.value - 300)
    gameEnemies.value = []
    gameBursts.value = []
  }

  resolveHits()
  gameFrameId = window.requestAnimationFrame(updateGame)
}

// Toggle theme
function toggleTheme() {
  isDark.value = !isDark.value
  document.documentElement.classList.toggle('dark', isDark.value)
  localStorage.setItem('theme', isDark.value ? 'dark' : 'light')
}

// Initialize theme
function initTheme() {
  const savedTheme = localStorage.getItem('theme')
  if (
    savedTheme === 'dark' ||
    (!savedTheme && window.matchMedia('(prefers-color-scheme: dark)').matches)
  ) {
    isDark.value = true
    document.documentElement.classList.add('dark')
  }
}

onMounted(() => {
  initTheme()

  // Check auth state
  authStore.checkAuth()

  // Ensure public settings are loaded (will use cache if already loaded from injected config)
  if (!appStore.publicSettingsLoaded) {
    appStore.fetchPublicSettings()
  }

  gameFrameId = window.requestAnimationFrame((now) => {
    lastFrameAt = now
    lastEnemyAt = now - 900
    updateGame(now)
  })
})

onBeforeUnmount(() => {
  if (gameFrameId) {
    window.cancelAnimationFrame(gameFrameId)
  }
})
</script>

<style scoped>
.home-page {
  --bg: #f8fafc;
  --surface: rgba(255, 255, 255, 0.9);
  --surface-strong: #ffffff;
  --text: #111827;
  --muted: #64748b;
  --line: rgba(17, 24, 39, 0.1);
  --accent: #f43f5e;
  --accent-2: #fbbf24;
  --accent-3: #38bdf8;
  --accent-soft: rgba(251, 191, 36, 0.16);
  --shadow: 0 3px 0 rgba(17, 24, 39, 0.16), 0 12px 28px rgba(17, 24, 39, 0.06);

  position: relative;
  min-height: 100vh;
  overflow: hidden;
  color: var(--text);
  background:
    linear-gradient(45deg, rgba(244, 63, 94, 0.05) 25%, transparent 25%),
    linear-gradient(-45deg, rgba(14, 165, 233, 0.05) 25%, transparent 25%),
    linear-gradient(45deg, transparent 75%, rgba(16, 185, 129, 0.05) 75%),
    linear-gradient(-45deg, transparent 75%, rgba(251, 191, 36, 0.08) 75%),
    #f8fafc;
  background-position: 0 0, 0 10px, 10px -10px, -10px 0, 0 0;
  background-size: 20px 20px, 20px 20px, 20px 20px, 20px 20px, auto;
  font-family:
    "Segoe UI",
    "PingFang SC",
    "Microsoft YaHei",
    system-ui,
    sans-serif;
  letter-spacing: 0;
}

.home-page::before {
  position: absolute;
  inset: 0;
  background:
    radial-gradient(circle at 14% 18%, rgba(244, 63, 94, 0.12), transparent 30%),
    radial-gradient(circle at 78% 20%, rgba(56, 189, 248, 0.12), transparent 28%),
    radial-gradient(circle at 86% 78%, rgba(251, 191, 36, 0.14), transparent 30%);
  content: "";
  pointer-events: none;
}

:global(html.dark .home-page) {
  --surface: rgba(15, 23, 42, 0.78);
  --surface-strong: #111827;
  --text: #f6f8ff;
  --muted: #94a3b8;
  --line: rgba(148, 163, 184, 0.16);
  --accent: #fb7185;
  --accent-2: #fbbf24;
  --accent-3: #38bdf8;
  --accent-soft: rgba(251, 191, 36, 0.14);
  --shadow: 0 3px 0 rgba(0, 0, 0, 0.3), 0 18px 36px rgba(0, 0, 0, 0.28);

  background:
    linear-gradient(45deg, rgba(244, 63, 94, 0.08) 25%, transparent 25%),
    linear-gradient(-45deg, rgba(56, 189, 248, 0.07) 25%, transparent 25%),
    linear-gradient(45deg, transparent 75%, rgba(16, 185, 129, 0.06) 75%),
    linear-gradient(-45deg, transparent 75%, rgba(251, 191, 36, 0.08) 75%),
    #020617;
  background-position: 0 0, 0 10px, 10px -10px, -10px 0, 0 0;
  background-size: 20px 20px, 20px 20px, 20px 20px, 20px 20px, auto;
}

:global(html.dark .home-page)::before {
  background:
    radial-gradient(circle at 14% 18%, rgba(244, 63, 94, 0.14), transparent 30%),
    radial-gradient(circle at 78% 20%, rgba(56, 189, 248, 0.12), transparent 28%),
    radial-gradient(circle at 86% 78%, rgba(251, 191, 36, 0.12), transparent 30%);
  opacity: 0.88;
}

.home-shell {
  position: relative;
  z-index: 1;
  width: min(1180px, calc(100% - 36px));
  margin: 0 auto;
}

.home-nav {
  z-index: 20;
  display: flex;
  min-height: 82px;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}

.brand {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  color: var(--text);
  font-size: 1.15rem;
  font-weight: 760;
}

.brand-mark {
  display: grid;
  width: 38px;
  height: 38px;
  place-items: center;
  overflow: hidden;
  border: 1px solid rgba(17, 24, 39, 0.14);
  border-radius: 10px;
  background: #ffffff;
  box-shadow: 0 3px 0 rgba(17, 24, 39, 0.16);
}

:global(html.dark .home-page .brand-mark) {
  border-color: rgba(148, 163, 184, 0.18);
  background: rgba(15, 23, 42, 0.72);
  box-shadow: 0 3px 0 rgba(0, 0, 0, 0.32);
}

.brand-mark img,
.brand-mark svg {
  width: 100%;
  height: 100%;
  object-fit: contain;
}

.home-nav-links {
  display: none;
  align-items: center;
  gap: 42px;
  color: var(--muted);
  font-size: 0.95rem;
  font-weight: 650;
}

.home-nav-links a {
  position: relative;
  transition: color 180ms ease;
}

.home-nav-links a:hover,
.home-nav-links a.router-link-active {
  color: var(--accent);
}

.home-nav-links a.router-link-active::after {
  position: absolute;
  right: 0;
  bottom: -30px;
  left: 0;
  height: 2px;
  border-radius: 99px;
  background: var(--accent);
  content: "";
}

.home-actions {
  position: relative;
  z-index: 21;
  display: flex;
  align-items: center;
  gap: 10px;
}

.icon-action {
  display: inline-flex;
  min-width: 44px;
  min-height: 44px;
  align-items: center;
  justify-content: center;
  border: 1px solid rgba(107, 155, 240, 0.14);
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.56);
  color: var(--muted);
  transition:
    color 160ms ease,
    background 160ms ease,
    transform 160ms ease;
  backdrop-filter: blur(14px);
}

:global(html.dark .home-page .icon-action) {
  border-color: rgba(148, 163, 184, 0.18);
  background: rgba(15, 23, 42, 0.7);
  color: #c7d2fe;
}

.icon-action:hover {
  transform: translateY(-1px);
  color: var(--accent);
  background: rgba(255, 255, 255, 0.78);
}

.button {
  display: inline-flex;
  min-height: 46px;
  align-items: center;
  justify-content: center;
  gap: 9px;
  border: 1px solid transparent;
  border-radius: 8px;
  padding: 0 22px;
  cursor: pointer;
  font-size: 0.95rem;
  font-weight: 740;
  white-space: nowrap;
  transition:
    transform 180ms ease,
    box-shadow 180ms ease,
    background 180ms ease,
    border-color 180ms ease;
}

.button:hover {
  transform: translateY(-2px);
}

.button.primary {
  border-color: rgba(17, 24, 39, 0.14);
  background: linear-gradient(135deg, #f43f5e, #fbbf24);
  color: #fff;
  box-shadow: 0 3px 0 rgba(17, 24, 39, 0.16), 0 14px 30px rgba(244, 63, 94, 0.14);
  text-shadow: 0 1px 0 rgba(17, 24, 39, 0.22);
}

.button.secondary {
  border-color: var(--line);
  background: rgba(255, 255, 255, 0.9);
  color: var(--text);
  box-shadow: 0 3px 0 rgba(17, 24, 39, 0.12);
}

:global(html.dark .home-page .button.primary) {
  background: linear-gradient(135deg, #f43f5e, #fbbf24);
  color: #ffffff;
  box-shadow: 0 3px 0 rgba(0, 0, 0, 0.35), 0 16px 34px rgba(244, 63, 94, 0.18);
}

:global(html.dark .home-page .button.secondary) {
  border-color: rgba(148, 163, 184, 0.18);
  background: rgba(15, 23, 42, 0.62);
  color: #e5edff;
}

.user-dot {
  display: grid;
  width: 22px;
  height: 22px;
  place-items: center;
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.18);
  font-size: 0.72rem;
}

.hero {
  display: grid;
  min-height: 570px;
  align-items: center;
  gap: 52px;
  padding: 52px 0 34px;
}

.copy {
  max-width: 640px;
}

.eyebrow {
  display: inline-flex;
  width: fit-content;
  align-items: center;
  gap: 8px;
  border-radius: 999px;
  background: var(--accent-soft);
  color: var(--accent);
  padding: 9px 15px;
  font-size: 0.9rem;
  font-weight: 760;
  animation: rise 620ms ease both;
}

.eyebrow svg {
  width: 16px;
  height: 16px;
}

.home-title {
  max-width: 620px;
  margin: 30px 0 20px;
  font-size: clamp(2.75rem, 7vw, 4.25rem);
  font-weight: 860;
  line-height: 1.06;
  letter-spacing: 0;
  animation: rise 700ms 80ms ease both;
}

.home-title span {
  display: inline-block;
}

.flow-text {
  background: linear-gradient(100deg, #f43f5e 0%, #fbbf24 32%, #38bdf8 66%, #f43f5e 100%);
  background-size: 260% 100%;
  -webkit-background-clip: text;
  background-clip: text;
  color: transparent;
  animation: text-flow 4.8s ease-in-out infinite;
}

:global(html.dark .home-page .flow-text) {
  background-image: linear-gradient(100deg, #fb7185 0%, #fbbf24 32%, #38bdf8 66%, #fb7185 100%);
}

.lead {
  max-width: 560px;
  margin: 0 0 34px;
  color: var(--muted);
  font-size: clamp(1rem, 1.8vw, 1.25rem);
  font-weight: 460;
  line-height: 1.75;
  animation: rise 760ms 150ms ease both;
}

.hero-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 16px;
  align-items: center;
  animation: rise 820ms 220ms ease both;
}

.visual {
  position: relative;
  z-index: 1;
  display: flex;
  min-height: 480px;
  align-items: center;
  justify-content: center;
  perspective: 1200px;
  animation: rise 780ms 160ms ease both;
}

.pixel-decor {
  position: absolute;
  z-index: -1;
  width: 44px;
  height: 44px;
  border: 3px solid #111827;
  border-radius: 8px;
  background: #fef3c7;
  box-shadow:
    8px 8px 0 #38bdf8,
    14px 14px 0 rgba(17, 24, 39, 0.12);
  image-rendering: pixelated;
}

.pixel-decor-one {
  top: 10%;
  right: 15%;
}

.pixel-decor-two {
  top: 25%;
  right: 5%;
  width: 22px;
  height: 22px;
  background: #f43f5e;
  box-shadow:
    7px 7px 0 #fbbf24,
    12px 12px 0 rgba(17, 24, 39, 0.12);
}

.pixel-decor-three {
  right: -2%;
  bottom: 35%;
  width: 28px;
  height: 28px;
  background: #10b981;
  box-shadow:
    7px 7px 0 #f43f5e,
    12px 12px 0 rgba(17, 24, 39, 0.12);
}

.console-card {
  width: min(100%, 720px);
  border: 1px solid rgba(17, 24, 39, 0.12);
  border-radius: 8px;
  background: rgba(255, 255, 255, 0.88);
  box-shadow:
    -18px 24px 40px rgba(17, 24, 39, 0.1),
    0 4px 0 rgba(17, 24, 39, 0.18);
  transform: rotateX(12deg) rotateY(-16deg) rotateZ(4deg);
  transform-style: preserve-3d;
  transition: transform 500ms cubic-bezier(0.175, 0.885, 0.32, 1.275);
  backdrop-filter: blur(18px);
}

:global(html.dark .home-page .console-card) {
  border-color: rgba(148, 163, 184, 0.18);
  background: rgba(15, 23, 42, 0.74);
  box-shadow:
    -25px 35px 70px rgba(0, 0, 0, 0.34),
    -10px 15px 28px rgba(92, 140, 242, 0.08),
    inset 0 0 0 1px rgba(148, 163, 184, 0.08);
}

.console-card:hover {
  transform: rotateX(8deg) rotateY(-10deg) rotateZ(2deg) translateY(-10px);
}

.console-inner {
  display: grid;
  grid-template-columns: 78px 1fr;
  min-height: 420px;
}

.side-rail {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 16px;
  border-right: 1px solid rgba(15, 23, 42, 0.05);
  border-radius: 8px 0 0 8px;
  background: rgba(255, 255, 255, 0.6);
  padding: 24px 0;
}

:global(html.dark .home-page .side-rail) {
  border-right-color: rgba(148, 163, 184, 0.1);
  background: rgba(2, 6, 23, 0.28);
}

.rail-logo {
  display: grid;
  width: 32px;
  height: 32px;
  place-items: center;
  overflow: hidden;
  margin-bottom: 12px;
  border: 1px solid rgba(17, 24, 39, 0.12);
  border-radius: 8px;
  background: #ffffff;
  box-shadow: 0 3px 0 rgba(17, 24, 39, 0.14);
}

.rail-logo img,
.rail-logo svg {
  width: 100%;
  height: 100%;
  object-fit: contain;
}

:global(html.dark .home-page .rail-logo) {
  background: rgba(15, 23, 42, 0.72);
  box-shadow: 0 10px 24px rgba(0, 0, 0, 0.24);
}

.side-rail i {
  display: flex;
  width: 40px;
  height: 40px;
  align-items: center;
  justify-content: center;
  border-radius: 8px;
  color: var(--muted);
}

.side-rail i.active {
  background: var(--accent-soft);
  color: var(--accent);
  box-shadow: 0 3px 0 rgba(17, 24, 39, 0.12);
}

.side-rail svg {
  width: 20px;
  height: 20px;
  stroke-width: 2;
}

.dashboard-board {
  padding: 24px;
}

.token-arcade {
  outline: none;
}

.arcade-bezel {
  overflow: hidden;
  border: 4px solid #202436;
  border-radius: 22px;
  background:
    linear-gradient(135deg, #ecf3ff 0%, #ccd8f7 45%, #f7fbff 46%, #b8c3dc 100%);
  box-shadow:
    inset 0 0 0 4px rgba(255, 255, 255, 0.68),
    0 22px 46px rgba(16, 24, 40, 0.18);
}

:global(html.dark .home-page .arcade-bezel) {
  border-color: #0f172a;
  background:
    linear-gradient(135deg, #27324a 0%, #141b2c 45%, #33415f 46%, #0e1525 100%);
  box-shadow:
    inset 0 0 0 4px rgba(148, 163, 184, 0.12),
    0 24px 50px rgba(0, 0, 0, 0.4);
}

.arcade-topline {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 10px;
  border-bottom: 4px solid #202436;
  background: #f8dc5c;
  color: #1f2937;
  padding: 10px 14px;
  font-family: "Segoe UI", "Microsoft YaHei", system-ui, sans-serif;
  font-size: 0.76rem;
  font-weight: 900;
  letter-spacing: 0;
  text-transform: uppercase;
}

.arcade-topline span:nth-child(2) {
  color: #d33232;
  text-align: center;
}

.arcade-topline span:nth-child(3) {
  color: #176c42;
  text-align: right;
}

.arcade-screen {
  position: relative;
  height: 310px;
  overflow: hidden;
  border-bottom: 4px solid #202436;
  cursor: crosshair;
  background:
    linear-gradient(to bottom, rgba(255, 255, 255, 0.06), transparent 50%),
    linear-gradient(90deg, rgba(255, 255, 255, 0.04) 1px, transparent 1px),
    linear-gradient(180deg, rgba(255, 255, 255, 0.04) 1px, transparent 1px),
    linear-gradient(180deg, #081225 0%, #132c56 58%, #1b5f7a 59%, #26364c 100%);
  background-size: auto, 20px 20px, 20px 20px, auto;
  image-rendering: pixelated;
}

.arcade-screen::before {
  position: absolute;
  inset: 0;
  z-index: 20;
  background: repeating-linear-gradient(
    to bottom,
    rgba(255, 255, 255, 0.08) 0,
    rgba(255, 255, 255, 0.08) 1px,
    transparent 1px,
    transparent 4px
  );
  content: "";
  mix-blend-mode: screen;
  opacity: 0.36;
  pointer-events: none;
}

.arcade-screen::after {
  position: absolute;
  inset: 0;
  z-index: 19;
  background: radial-gradient(circle at 50% 44%, transparent 0 48%, rgba(0, 0, 0, 0.3) 100%);
  content: "";
  pointer-events: none;
}

.pixel-stars {
  position: absolute;
  inset: 0;
  background:
    radial-gradient(circle at 12% 22%, #fff 0 1px, transparent 1.5px),
    radial-gradient(circle at 28% 42%, #f8dc5c 0 1px, transparent 1.5px),
    radial-gradient(circle at 44% 18%, #fff 0 1px, transparent 1.5px),
    radial-gradient(circle at 69% 30%, #98f3ff 0 1px, transparent 1.5px),
    radial-gradient(circle at 84% 16%, #fff 0 1px, transparent 1.5px),
    radial-gradient(circle at 91% 48%, #f8dc5c 0 1px, transparent 1.5px);
  animation: star-scroll 5.8s linear infinite;
  opacity: 0.8;
}

.city-line {
  position: absolute;
  right: 0;
  bottom: 0;
  left: 0;
  display: flex;
  align-items: flex-end;
  gap: 10px;
  height: 34%;
  padding: 0 18px;
  opacity: 0.9;
}

.city-line span {
  width: 9%;
  background:
    repeating-linear-gradient(to bottom, rgba(248, 220, 92, 0.85) 0 4px, transparent 4px 12px),
    linear-gradient(180deg, #253556, #162238);
  box-shadow: inset 0 0 0 2px rgba(255, 255, 255, 0.05);
}

.hero-sprite,
.token-enemy,
.light-beam,
.token-burst {
  position: absolute;
  z-index: 4;
  transform: translate(-50%, -50%);
}

.hero-sprite {
  width: 66px;
  height: 102px;
  filter:
    drop-shadow(0 0 12px rgba(129, 222, 255, 0.74))
    drop-shadow(0 16px 0 rgba(0, 0, 0, 0.16));
  transition: left 80ms linear, top 80ms linear;
}

.hero-head {
  position: absolute;
  top: 0;
  left: 18px;
  width: 30px;
  height: 26px;
  border: 3px solid #1e293b;
  border-radius: 12px 12px 6px 6px;
  background: linear-gradient(90deg, #d9e3ef 0 35%, #d33232 35% 58%, #eef6ff 58%);
}

.hero-head::before,
.hero-head::after {
  position: absolute;
  top: 8px;
  width: 5px;
  height: 5px;
  background: #f8dc5c;
  box-shadow: 0 0 8px #f8dc5c;
  content: "";
}

.hero-head::before {
  left: 5px;
}

.hero-head::after {
  right: 5px;
}

.hero-core {
  position: absolute;
  top: 34px;
  left: 28px;
  z-index: 4;
  width: 11px;
  height: 11px;
  border: 2px solid #c8f7ff;
  border-radius: 50%;
  background: #54e0ff;
  box-shadow: 0 0 14px #54e0ff;
  animation: core-pulse 900ms ease-in-out infinite;
}

.hero-body {
  position: absolute;
  top: 24px;
  left: 17px;
  width: 32px;
  height: 50px;
  border: 3px solid #1e293b;
  clip-path: polygon(12% 0, 88% 0, 100% 100%, 0 100%);
  background:
    linear-gradient(120deg, transparent 0 28%, #d33232 28% 46%, transparent 46%),
    linear-gradient(240deg, transparent 0 30%, #d33232 30% 47%, transparent 47%),
    #e8eef7;
}

.hero-arm {
  position: absolute;
  top: 32px;
  width: 17px;
  height: 42px;
  border: 3px solid #1e293b;
  border-radius: 8px;
  background: linear-gradient(180deg, #e8eef7 0 45%, #d33232 45%);
}

.hero-arm-left {
  left: 6px;
  transform: rotate(18deg);
}

.hero-arm-right {
  right: 0;
  transform: rotate(-68deg);
  transform-origin: 50% 20%;
}

.hero-leg {
  position: absolute;
  top: 70px;
  width: 17px;
  height: 31px;
  border: 3px solid #1e293b;
  border-radius: 7px;
  background: linear-gradient(180deg, #d33232, #e8eef7 54%);
}

.hero-leg-left {
  left: 17px;
}

.hero-leg-right {
  right: 13px;
}

.light-beam {
  width: 78px;
  height: 8px;
  border: 2px solid rgba(255, 255, 255, 0.82);
  background: linear-gradient(90deg, #fff, #f8dc5c 38%, #54e0ff 78%, transparent);
  box-shadow:
    0 0 12px #54e0ff,
    0 0 26px rgba(248, 220, 92, 0.7);
}

.token-enemy {
  display: grid;
  width: 50px;
  height: 46px;
  place-items: center;
  border: 3px solid #381b2d;
  border-radius: 6px;
  background:
    linear-gradient(135deg, #ff5a5a 0 45%, #f8dc5c 45% 58%, #8b1e3f 58%),
    #d33232;
  color: #fff9d9;
  font-size: 0.72rem;
  font-weight: 950;
  text-shadow: 2px 2px 0 #381b2d;
  box-shadow:
    inset -5px -5px 0 rgba(0, 0, 0, 0.18),
    0 0 18px rgba(255, 90, 90, 0.45);
  animation: token-wobble var(--wobble) steps(2) infinite;
}

.token-enemy::before,
.token-enemy::after {
  position: absolute;
  top: -8px;
  width: 11px;
  height: 11px;
  border: 3px solid #381b2d;
  border-radius: 50%;
  background: #f8dc5c;
  content: "";
}

.token-enemy::before {
  left: 6px;
}

.token-enemy::after {
  right: 6px;
}

.token-burst {
  width: 54px;
  height: 54px;
  background:
    radial-gradient(circle, #fff 0 12%, #f8dc5c 13% 28%, #ff5a5a 29% 44%, transparent 46%),
    conic-gradient(from 0deg, transparent 0 10%, #54e0ff 10% 16%, transparent 16% 30%, #f8dc5c 30% 36%, transparent 36% 58%, #fff 58% 64%, transparent 64%);
  filter: drop-shadow(0 0 16px rgba(248, 220, 92, 0.8));
  animation: burst-pop 520ms steps(4) forwards;
}

.arcade-panel {
  display: grid;
  grid-template-columns: 1fr auto;
  gap: 16px;
  align-items: center;
  background: #e7edf7;
  padding: 16px;
}

:global(html.dark .home-page .arcade-panel) {
  background: #151e31;
}

.power-meter {
  height: 18px;
  overflow: hidden;
  border: 3px solid #202436;
  border-radius: 4px;
  background: #202436;
}

.power-meter span {
  display: block;
  height: 100%;
  background: repeating-linear-gradient(
    90deg,
    #2fd17c 0 14px,
    #f8dc5c 14px 20px,
    #2fd17c 20px 34px
  );
  transition: width 140ms linear;
}

.arcade-controls {
  display: grid;
  grid-template-columns: repeat(4, 32px);
  gap: 7px;
}

.arcade-controls button {
  display: grid;
  width: 32px;
  height: 32px;
  place-items: center;
  border: 3px solid #202436;
  border-radius: 50%;
  background: #d33232;
  color: #fff;
  font-size: 0.7rem;
  font-weight: 900;
  line-height: 1;
  box-shadow: inset -3px -4px 0 rgba(0, 0, 0, 0.22);
}

:global(html.dark .home-page .step-card) {
  border-color: rgba(148, 163, 184, 0.12);
  background: rgba(15, 23, 42, 0.82);
  box-shadow: 0 16px 38px rgba(0, 0, 0, 0.18);
}

.step-strip {
  margin: 20px auto 72px;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: rgba(255, 255, 255, 0.88);
  padding: 40px 32px;
  box-shadow: var(--shadow);
  backdrop-filter: blur(18px);
}

:global(html.dark .home-page .step-strip) {
  border-color: rgba(148, 163, 184, 0.16);
  background: rgba(15, 23, 42, 0.58);
}

.step-strip h2 {
  margin: 0 0 32px;
  text-align: center;
  font-size: 1.25rem;
  font-weight: 840;
}

.steps {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}

.step-card {
  position: relative;
  display: flex;
  min-height: 104px;
  flex: 1;
  align-items: center;
  gap: 16px;
  border: 1px solid rgba(15, 23, 42, 0.04);
  border-radius: 8px;
  background: #ffffff;
  padding: 20px 24px;
  box-shadow: 0 3px 0 rgba(17, 24, 39, 0.12), 0 12px 24px rgba(17, 24, 39, 0.04);
  transition: transform 300ms ease;
}

.step-card:hover {
  transform: translateY(-3px);
}

.step-num {
  position: absolute;
  top: -12px;
  left: 52px;
  display: grid;
  width: 24px;
  height: 24px;
  place-items: center;
  border-radius: 50%;
  background: var(--accent);
  box-shadow: 0 0 0 4px #ffffff;
  color: #fff;
  font-size: 0.75rem;
  font-weight: 800;
  transform: translateX(-50%);
}

:global(html.dark .home-page .step-num) {
  box-shadow: 0 0 0 4px #111827;
}

.step-icon {
  display: grid;
  width: 56px;
  height: 56px;
  flex-shrink: 0;
  place-items: center;
  border-radius: 8px;
  background: var(--accent-soft);
  color: var(--accent);
}

.step-icon svg {
  width: 28px;
  height: 28px;
}

.step-card h3 {
  margin: 0 0 6px;
  color: var(--text);
  font-size: 1.06rem;
  font-weight: 760;
}

.step-card p {
  margin: 0;
  color: var(--muted);
  font-size: 0.82rem;
  font-weight: 500;
  line-height: 1.5;
}

.step-arrow {
  position: relative;
  width: 32px;
  height: 2px;
  flex-shrink: 0;
  background-image: linear-gradient(to right, #cbd5e1 50%, transparent 50%);
  background-size: 8px 100%;
}

.step-arrow::after {
  position: absolute;
  top: 50%;
  right: -4px;
  border-width: 4px 0 4px 6px;
  border-style: solid;
  border-color: transparent transparent transparent #cbd5e1;
  content: "";
  transform: translateY(-50%);
}

.home-footer {
  display: flex;
  justify-content: center;
  padding: 5px 0 5px;
  color: #8da0b7;
  font-size: 0.88rem;
  text-align: center;
}

:global(html.dark .home-page .home-footer) {
  color: #8fa3c0;
}

@keyframes ambient-shift {
  0% {
    transform: translate3d(-2%, -1%, 0) scale(1);
  }
  100% {
    transform: translate3d(2%, 1%, 0) scale(1.04);
  }
}

@keyframes rise {
  from {
    opacity: 0;
    transform: translateY(16px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}

@keyframes text-flow {
  0%,
  100% {
    background-position: 0% 50%;
  }
  50% {
    background-position: 100% 50%;
  }
}

@keyframes star-scroll {
  from {
    transform: translateX(0);
  }
  to {
    transform: translateX(-22px);
  }
}

@keyframes core-pulse {
  0%,
  100% {
    transform: scale(1);
  }
  50% {
    transform: scale(1.22);
  }
}

@keyframes token-wobble {
  0%,
  100% {
    transform: translate(-50%, -50%) rotate(-3deg);
  }
  50% {
    transform: translate(-50%, -50%) rotate(3deg);
  }
}

@keyframes burst-pop {
  from {
    transform: translate(-50%, -50%) scale(0.55) rotate(0deg);
  }
  to {
    transform: translate(-50%, -50%) scale(1.25) rotate(28deg);
  }
}

@media (min-width: 768px) {
  .home-shell {
    width: min(1180px, calc(100% - 48px));
  }

  .home-nav-links {
    display: flex;
  }

  .hero {
    grid-template-columns: minmax(0, 0.92fr) minmax(440px, 1.08fr);
  }
}

@media (max-width: 900px) {
  .hero {
    grid-template-columns: 1fr;
    min-height: auto;
    gap: 26px;
    padding: 42px 0 28px;
  }

  .visual {
    perspective: none;
  }

  .console-card {
    transform: none !important;
  }

  .pixel-decor {
    display: none;
  }

  .arcade-screen {
    height: 300px;
  }

  .steps {
    flex-direction: column;
    align-items: stretch;
    gap: 24px;
  }

  .step-arrow {
    display: none;
  }

  .step-num {
    left: 50%;
  }
}

@media (max-width: 640px) {
  .home-actions :deep(.locale-switcher) {
    display: none;
  }

  .home-nav {
    min-height: 72px;
  }

  .brand span:last-child {
    max-width: 126px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .nav-cta {
    display: none;
  }

  .visual {
    min-height: auto;
  }

  .console-inner {
    grid-template-columns: 1fr;
    min-height: auto;
  }

  .side-rail {
    display: none;
  }

  .dashboard-board {
    padding: 20px;
  }

  .arcade-topline {
    grid-template-columns: 1fr;
    gap: 3px;
  }

  .arcade-topline span,
  .arcade-topline span:nth-child(2),
  .arcade-topline span:nth-child(3) {
    text-align: left;
  }

  .arcade-screen {
    height: 280px;
  }

  .arcade-panel {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 430px) {
  .home-shell {
    width: min(100% - 28px, 1180px);
  }

  .home-title {
    font-size: 2.6rem;
  }

  .hero-actions .button {
    width: 100%;
  }

  .hero-sprite {
    width: 56px;
    height: 88px;
    transform: translate(-50%, -50%) scale(0.86);
  }

  .step-strip {
    padding: 34px 18px;
  }
}

@media (prefers-reduced-motion: reduce) {
  *,
  *::before,
  *::after {
    animation-duration: 0.01ms !important;
    animation-iteration-count: 1 !important;
    scroll-behavior: auto !important;
    transition-duration: 0.01ms !important;
  }
}
</style>
