declare module "civic-validators" {
  const validators: Record<string, (value: unknown) => boolean>;
  export = validators;
}
