import { SALES_SCHEMA, type BusinessDocument } from '../domain/sales'
import { useDocuments, type DocumentsData } from './useDocuments'

export type SalesData = DocumentsData<BusinessDocument>

/** The site writes its summary hourly; a five-minute refresh keeps an open tab current. */
const REFRESH_MS = 5 * 60_000

// A summary of another schema is shown as unknown, never misread as this one.
const isSales = (d: BusinessDocument) => d.body?.schema === SALES_SCHEMA

/** Fetches the latest business summaries (Change 27) and refreshes them while the view is open. */
export function useSales(): SalesData {
  return useDocuments<BusinessDocument>('business', REFRESH_MS, isSales)
}
