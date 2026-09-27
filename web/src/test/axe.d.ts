import 'vitest'

// vitest-axe 0.1 aumenta el namespace antiguo `Vi`; vitest 5 espera el módulo `vitest`.
declare module 'vitest' {
  interface Assertion {
    toHaveNoViolations(): void
  }
  interface AsymmetricMatchersContaining {
    toHaveNoViolations(): void
  }
}
