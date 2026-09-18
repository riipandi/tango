// Outbound webhook endpoints and their delivery records.

import { toPaginated } from '../pagination'
import type {
  CreateWebhookParams,
  UpdateWebhookParams,
  Webhook,
  WebhookDelivery
} from '../schemas/webhook.schema'
import type { CallOptions, Executor, Paginated } from '../types'

export interface WebhookListParams {
  enabled?: boolean
  event?: string
  page?: number
  limit?: number
}

export interface DeliveryListParams {
  page?: number
  limit?: number
}

export interface WebhooksModule {
  list(params?: WebhookListParams): Promise<Paginated<Webhook>>
  get(webhookId: string): Promise<Webhook>
  create(params: CreateWebhookParams): Promise<Webhook>
  update(webhookId: string, params: UpdateWebhookParams): Promise<Webhook>
  remove(webhookId: string): Promise<void>
  /** Sends a test delivery (202); answers with its delivery id. */
  sendTest(webhookId: string): Promise<{ delivery_id: string }>
  /** Rotates the signing secret; the plaintext appears exactly once. */
  rotateSecret(webhookId: string): Promise<Webhook>
  /** All deliveries across endpoints. */
  listDeliveries(params?: DeliveryListParams): Promise<Paginated<WebhookDelivery>>
  /** Deliveries of one endpoint, newest first. */
  listDeliveriesFor(
    webhookId: string,
    params?: DeliveryListParams
  ): Promise<Paginated<WebhookDelivery>>
}

export function createWebhooksModule(exec: Executor): WebhooksModule {
  const deliveryOptions = (params?: DeliveryListParams): CallOptions | undefined =>
    params ? { query: { page: params.page, limit: params.limit } } : undefined

  return {
    list: (params) =>
      exec
        .get<Webhook[]>('/webhooks', {
          query: {
            enabled: params?.enabled,
            event: params?.event,
            page: params?.page,
            limit: params?.limit
          }
        })
        .then(toPaginated),
    get: (webhookId) => exec.get<Webhook>(`/webhooks/${webhookId}`).then((r) => r.data),
    create: (params) => exec.post<Webhook>('/webhooks', params).then((r) => r.data),
    update: (webhookId, params) =>
      exec.put<Webhook>(`/webhooks/${webhookId}`, params).then((r) => r.data),
    remove: (webhookId) => exec.delete(`/webhooks/${webhookId}`).then(() => undefined),
    sendTest: (webhookId) =>
      exec.post<{ delivery_id: string }>(`/webhooks/${webhookId}/test`).then((r) => r.data),
    rotateSecret: (webhookId) =>
      exec.post<Webhook>(`/webhooks/${webhookId}/rotate-secret`).then((r) => r.data),
    listDeliveries: (params) =>
      exec.get<WebhookDelivery[]>('/webhook-deliveries', deliveryOptions(params)).then(toPaginated),
    listDeliveriesFor: (webhookId, params) =>
      exec
        .get<WebhookDelivery[]>(`/webhooks/${webhookId}/deliveries`, deliveryOptions(params))
        .then(toPaginated)
  }
}
