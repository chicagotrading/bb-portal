type InvocationWithInstanceName = {
  instanceName?: { name?: string | null } | null;
};

export const showInstanceNameColumn = (
  invocations: InvocationWithInstanceName[],
  filterActive: boolean,
): boolean => {
  // An active filter keeps the column so it can be cleared.
  if (filterActive) {
    return true;
  }
  const names = new Set(
    invocations.map((invocation) => invocation.instanceName?.name || ""),
  );
  return names.size > 1;
};
