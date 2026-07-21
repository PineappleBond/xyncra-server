import { inject } from 'vue'
import { XyncraClientKey } from '../plugin'

export function useXyncra() {
  const context = inject(XyncraClientKey)
  if (!context) {
    throw new Error(
      'useXyncra must be used within a Vue app that has the Xyncra plugin installed. ' +
      'Make sure to app.use(createXyncraPlugin()) before mounting your app.',
    )
  }
  return context
}
