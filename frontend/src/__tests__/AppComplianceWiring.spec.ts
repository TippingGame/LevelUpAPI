import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const appPath = resolve(dirname(fileURLToPath(import.meta.url)), '../App.vue')
const appSource = readFileSync(appPath, 'utf8')

describe('App admin compliance wiring', () => {
  it('mounts and initializes the blocking acknowledgement flow', () => {
    expect(appSource).toContain('<AdminComplianceDialog />')
    expect(appSource).toContain('adminComplianceStore.fetchStatus()')
    expect(appSource).toContain('adminComplianceStore.requireAcknowledgement(detail)')
    expect(appSource).toContain('adminComplianceStore.reset()')
  })

  it('registers and removes the compliance-required listener', () => {
    expect(appSource).toContain("window.addEventListener('admin-compliance-required', onAdminComplianceRequired)")
    expect(appSource).toContain("window.removeEventListener('admin-compliance-required', onAdminComplianceRequired)")
  })

  it('reloads one-shot admin data after acknowledgement unlocks the API', () => {
    const dialogPath = resolve(dirname(fileURLToPath(import.meta.url)), '../components/admin/AdminComplianceDialog.vue')
    const dialogSource = readFileSync(dialogPath, 'utf8')
    expect(dialogSource).toContain('window.location.reload()')
  })
})
