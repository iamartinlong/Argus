import { create } from "zustand"

export interface AuthUser {
  id: string
  username: string
  role: string
}

interface AuthState {
  accessToken: string | null
  user: AuthUser | null
  mustChangePassword: boolean
  setAuth: (accessToken: string, user: AuthUser, mustChangePassword?: boolean) => void
  clearAuth: () => void
  setMustChangePassword: (v: boolean) => void
}

export const useAuthStore = create<AuthState>((set) => ({
  accessToken: null,
  user: null,
  mustChangePassword: false,
  setAuth: (accessToken, user, mustChangePassword = false) =>
    set({ accessToken, user, mustChangePassword }),
  clearAuth: () => set({ accessToken: null, user: null, mustChangePassword: false }),
  setMustChangePassword: (v) => set({ mustChangePassword: v }),
}))
