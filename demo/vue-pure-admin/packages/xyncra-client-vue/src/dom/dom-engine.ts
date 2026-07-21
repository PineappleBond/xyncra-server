export function isHidden(el: Element): boolean {
  if (el.hasAttribute('hidden')) return true
  if (el.tagName === 'INPUT' && (el as HTMLInputElement).type === 'hidden') return true
  const htmlEl = el as HTMLElement
  const style = getComputedStyle(htmlEl)
  if (style.display === 'none') return true
  if (style.visibility === 'hidden') return true
  if (htmlEl.offsetParent === null) {
    if (style.position === 'fixed' || style.position === 'sticky') {
      return style.display === 'none' || style.visibility === 'hidden'
    }
    return true
  }
  return false
}
