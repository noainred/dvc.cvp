// TypeScript mirrors of the Go API JSON payloads.

export interface FamilyCount {
  family: string
  count: number
}

export interface Summary {
  dataCenters: number
  dataCentersOk: number
  devices: number
  devicesUp: number
  portTotal: number
  portUsed: number
  portFree: number
  portFreeReady: number
  portFreeEmpty: number
  portDisabled: number
  portError: number
  opticAlarms: number
  totalInBps: number
  totalOutBps: number
  byFamily: FamilyCount[]
  updatedAt: string
}

export interface DataCenter {
  id: string
  name: string
  region: string
  city: string
  proxyType: string
  status: string
  deviceCount: number
  deviceUp: number
  portTotal: number
  portUsed: number
  portFree: number
  portFreeReady: number
  opticAlarms: number
  inBps: number
  outBps: number
  maxUtilPct: number
  lastPoll: string
  error?: string
}

export interface Device {
  serial: string
  hostname: string
  model: string
  family: string
  version: string
  mgmtIp: string
  dataCenter: string
  role: string
  status: string
  uptimeSec: number
  portTotal: number
  portUp: number
  portUsed: number
  portFree: number
  portFreeReady: number
  portErr: number
  opticAlarms: number
  inBps: number
  outBps: number
  maxUtilPct: number
  lastPoll: string
}

export interface Interface {
  device: string
  name: string
  description: string
  adminStatus: string
  operStatus: string
  state: 'used' | 'free' | 'disabled' | 'error'
  speedBps: number
  inBps: number
  outBps: number
  inUtilPct: number
  outUtilPct: number
  utilPct: number
  inErrors: number
  outErrors: number
  inDiscards: number
  outDiscards: number
  lastChange: string
  neighbor?: string
  hasTransceiver: boolean
  mediaType?: string
  xcvrVendor?: string
  xcvrPart?: string
  xcvrSerial?: string
  domValid: boolean
  txPowerDbm?: number
  rxPowerDbm?: number
  tempC?: number
  voltageV?: number
  opticAlarm?: string
}

export interface Sample {
  t: string
  inBps: number
  outBps: number
}

export interface Alert {
  key: string
  severity: 'critical' | 'warning'
  type: 'util' | 'optic' | 'errors' | 'device' | 'datacenter'
  dataCenter?: string
  device?: string
  hostname?: string
  interface?: string
  message: string
  value?: number
  since: string
  active: boolean
}

export interface LivePayload {
  summary: Summary
  datacenters: DataCenter[]
  devices: Device[]
  alerts: Alert[]
}

export interface SummaryPoint {
  t: string
  portUsed: number
  portTotal: number
  inBps: number
  outBps: number
}

export interface Trend {
  points: number
  current: number
  slopePerDay: number
  threshold?: number
  daysToThreshold?: number
  reachAt?: string
}

export interface TrendResponse {
  series: SummaryPoint[]
  portUsage: Trend
  throughput: Trend
}

export interface DeviceTrendResponse {
  series: Sample[]
  throughput: Trend
}

export interface UpgradeStatus {
  current: string
  commit: string
  buildTime: string
  latest?: string
  notes?: string
  releaseUrl?: string
  upgradeAvailable: boolean
  enabled: boolean
  autoApply: boolean
  applying: boolean
  lastChecked?: string
  error?: string
}
