import { Store } from '@tanstack/store'

export const loginExampleStore = new Store({
  usernamePreview: '',
  submitCount: 0,
  lastSubmittedAt: '',
})

export function setUsernamePreview(usernamePreview: string) {
  loginExampleStore.setState((state) => ({
    ...state,
    usernamePreview,
  }))
}

export function markLoginSubmit() {
  loginExampleStore.setState((state) => ({
    ...state,
    submitCount: state.submitCount + 1,
    lastSubmittedAt: new Date().toISOString(),
  }))
}
