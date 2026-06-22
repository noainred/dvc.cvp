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
  portDisabled: number
  portError: number
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
  portErr: number
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
}

export interface Sample {
  t: string
  inBps: number
  outBps: number
}

export interface LivePayload {
  summary: Summary
  datacenters: DataCenter[]
  devices: Device[]
}
