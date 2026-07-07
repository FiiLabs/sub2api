/**
 * Ambient module for the `@phala/dcap-qvl-web` wasm asset imported with Vite's
 * `?url` suffix. Vite (`vite/client`) already declares a generic `*?url`
 * module, but declaring the exact specifier keeps `vue-tsc` unambiguous for the
 * lazy dynamic import in `tdxVerify.ts`.
 */
declare module '@phala/dcap-qvl-web/dcap-qvl-web_bg.wasm?url' {
  const url: string
  export default url
}
