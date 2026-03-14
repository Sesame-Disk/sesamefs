const SORT_KEY = 'sesamefs_sort_preference';

export type SortField = 'name' | 'date' | 'size';
export type SortDirection = 'asc' | 'desc';

export interface SortPreference {
  field: SortField;
  direction: SortDirection;
}

const DEFAULT_SORT: SortPreference = { field: 'name', direction: 'asc' };

export function getSortPreference(): SortPreference {
  try {
    const stored = localStorage.getItem(SORT_KEY);
    if (stored) {
      const parsed = JSON.parse(stored);
      if (parsed.field && parsed.direction) return parsed;
    }
  } catch {
    // ignore
  }
  return DEFAULT_SORT;
}

export function setSortPreference(pref: SortPreference): void {
  localStorage.setItem(SORT_KEY, JSON.stringify(pref));
}
