export interface StringStorage {
  getItem(key: string): string | null
  setItem(key: string, value: string): void
  removeItem(key: string): void
}

export class PurchaseIntentManager {
  constructor(
    private readonly storage: StringStorage,
    private readonly generateKey: () => string = defaultKey,
  ) {}

  current(bookId: number): string | null {
    return this.storage.getItem(this.storageKey(bookId))
  }

  begin(bookId: number): string {
    const existing = this.current(bookId)
    if (existing) return existing
    return this.restart(bookId)
  }

  restart(bookId: number): string {
    const key = this.generateKey()
    this.storage.setItem(this.storageKey(bookId), key)
    return key
  }

  complete(bookId: number, key: string): void {
    if (this.current(bookId) === key) this.storage.removeItem(this.storageKey(bookId))
  }

  private storageKey(bookId: number): string {
    return `bookloop.purchase_intent.${bookId}`
  }
}

function defaultKey(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') return crypto.randomUUID()
  return `${Date.now()}-${Math.random().toString(16).slice(2)}`
}

const fallbackValues = new Map<string, string>()
const fallbackStorage: StringStorage = {
  getItem: (key) => fallbackValues.get(key) ?? null,
  setItem: (key, value) => fallbackValues.set(key, value),
  removeItem: (key) => fallbackValues.delete(key),
}

export const purchaseIntents = new PurchaseIntentManager(
  typeof sessionStorage === 'undefined' ? fallbackStorage : sessionStorage,
)
