// swagger-ui-dist ships no types; ApiDocsPage only needs the bundle's default export.
declare module "swagger-ui-dist/swagger-ui-es-bundle.js" {
  const SwaggerUIBundle: (options: Record<string, unknown>) => unknown;
  export default SwaggerUIBundle;
}
