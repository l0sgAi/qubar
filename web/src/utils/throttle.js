export function useThrottleFn(fn, delay) {
  let lastCall = 0
  let timer = null

  return function (...args) {
    const now = Date.now()
    const remaining = delay - (now - lastCall)

    if (remaining <= 0) {
      if (timer) {
        clearTimeout(timer)
        timer = null
      }
      lastCall = now
      return fn.apply(this, args)
    }

    if (!timer) {
      timer = setTimeout(() => {
        lastCall = Date.now()
        timer = null
        fn.apply(this, args)
      }, remaining)
    }
  }
}

export function useDebounceFn(fn, delay) {
  let timer = null
  return function (...args) {
    if (timer) clearTimeout(timer)
    timer = setTimeout(() => {
      timer = null
      fn.apply(this, args)
    }, delay)
  }
}
