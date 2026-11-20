import type { components } from "./schema";

// Aliases over the generated schema; never hand-write API shapes.
type S = components["schemas"];
export type Endpoint = S["Endpoint"];
export type EndpointList = S["EndpointList"];
export type Project = S["Project"];
export type MessageSummary = S["MessageSummary"];
export type MessageList = S["MessageList"];
export type MessageDetail = S["MessageDetail"];
export type Delivery = S["Delivery"];
export type DeliveryStatus = S["DeliveryStatus"];
export type DeliveryCounts = S["DeliveryCounts"];
export type Attempt = S["Attempt"];
export type AttemptList = S["AttemptList"];
