import { z } from 'zod'

// Mirrors modules/webhook: Webhook DTO, create/update params, Delivery.
export const WebhookSchema = z.object({
  id: z.string(),
  name: z.string(),
  description: z.string().nullable().optional(),
  endpoint: z.string(),
  method: z.string(),
  headers: z.record(z.string(), z.string()).optional(),
  enabled: z.boolean(),
  event_types: z.array(z.string()),
  // Plaintext signing secret — create and rotate responses only.
  secret: z.string().optional(),
  created_at: z.string(),
  updated_at: z.string().nullable().optional()
})

export const CreateWebhookSchema = z.object({
  name: z.string(),
  description: z.string().nullable().optional(),
  endpoint: z.string(),
  method: z.string().optional(),
  headers: z.record(z.string(), z.string()).optional(),
  enabled: z.boolean().optional(),
  event_types: z.array(z.string()).optional()
})

export const UpdateWebhookSchema = z.object({
  name: z.string().nullable().optional(),
  description: z.string().nullable().optional(),
  endpoint: z.string().nullable().optional(),
  method: z.string().nullable().optional(),
  headers: z.record(z.string(), z.string()).optional(),
  enabled: z.boolean().optional(),
  event_types: z.array(z.string()).optional()
})

export const DeliverySchema = z.object({
  id: z.string(),
  webhook_id: z.string().nullable().optional(),
  event: z.string().nullable().optional(),
  http_status: z.number().nullable().optional(),
  response: z.record(z.string(), z.unknown()).optional(),
  attempts: z.number(),
  succeeded: z.boolean(),
  error: z.string().nullable().optional(),
  created_at: z.string(),
  delivered_at: z.string().nullable().optional()
})

export type Webhook = z.infer<typeof WebhookSchema>
export type CreateWebhookParams = z.infer<typeof CreateWebhookSchema>
export type UpdateWebhookParams = z.infer<typeof UpdateWebhookSchema>
export type WebhookDelivery = z.infer<typeof DeliverySchema>
