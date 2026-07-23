import { ref } from 'vue'

export interface AskUserState {
  visible: boolean
  question: string
  callingId: string
  resolve: ((answer: string) => void) | null
  reject: ((error: Error) => void) | null
}

export const askUserState = ref<AskUserState>({
  visible: false,
  question: '',
  callingId: '',
  resolve: null,
  reject: null,
})

export function openAskUserDialog(question: string, callingId: string): Promise<string> {
  return new Promise<string>((resolve, reject) => {
    askUserState.value = {
      visible: true,
      question,
      callingId,
      resolve,
      reject,
    }
  })
}

export function submitAskUserAnswer(answer: string): void {
  const state = askUserState.value
  if (state.resolve) {
    state.resolve(answer)
  }
  resetAskUserState()
}

export function cancelAskUser(): void {
  const state = askUserState.value
  if (state.reject) {
    state.reject(new Error('User cancelled'))
  }
  resetAskUserState()
}

function resetAskUserState(): void {
  askUserState.value = {
    visible: false,
    question: '',
    callingId: '',
    resolve: null,
    reject: null,
  }
}
