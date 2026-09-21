import { describe, expect, it } from 'vitest'
import { PurchaseIntentManager, type StringStorage } from './purchaseIntent'

class MemoryStorage implements StringStorage {
  values = new Map<string, string>()
  getItem(key: string) { return this.values.get(key) ?? null }
  setItem(key: string, value: string) { this.values.set(key, value) }
  removeItem(key: string) { this.values.delete(key) }
}

describe('PurchaseIntentManager', () => {
  it('reuses one key for retries until the purchase succeeds', () => {
    const storage = new MemoryStorage()
    let sequence = 0
    const intents = new PurchaseIntentManager(storage, () => `intent-${++sequence}`)

    const first = intents.begin(42)
    expect(intents.begin(42)).toBe(first)
    expect(intents.begin(7)).not.toBe(first)

    intents.complete(42, 'a-different-key')
    expect(intents.begin(42)).toBe(first)
    intents.complete(42, first)
    expect(intents.begin(42)).toBe('intent-3')
  })

  it('creates a new key only when the user explicitly restarts an intent', () => {
    const storage = new MemoryStorage()
    let sequence = 0
    const intents = new PurchaseIntentManager(storage, () => `intent-${++sequence}`)

    expect(intents.begin(1)).toBe('intent-1')
    expect(intents.restart(1)).toBe('intent-2')
    expect(intents.current(1)).toBe('intent-2')
  })
})

