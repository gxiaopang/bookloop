import type { User } from '@/types/api'

const tokenKey = 'bookloop.access_token'
const userKey = 'bookloop.user'

function readUser(): User | null {
  const raw = sessionStorage.getItem(userKey)
  if (!raw) return null
  try {
    return JSON.parse(raw) as User
  } catch {
    sessionStorage.removeItem(userKey)
    return null
  }
}

let accessToken = sessionStorage.getItem(tokenKey)

export const authSession = {
  getToken: () => accessToken,
  getUser: readUser,
  save(token: string, user: User) {
    accessToken = token
    sessionStorage.setItem(tokenKey, token)
    sessionStorage.setItem(userKey, JSON.stringify(user))
  },
  updateUser(user: User) {
    sessionStorage.setItem(userKey, JSON.stringify(user))
  },
  clear() {
    accessToken = null
    sessionStorage.removeItem(tokenKey)
    sessionStorage.removeItem(userKey)
  },
}

