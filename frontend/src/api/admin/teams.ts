/**
 * Admin Teams API endpoints
 * Handles team workspace oversight for platform administrators
 */

import { apiClient } from '../client'
import type {
  AdminTeam,
  AdminTeamMember,
  TeamInvitation,
  TeamRole,
  PaginatedResponse
} from '@/types'

/** Roles that can be assigned to a member via the admin API (owner is excluded). */
export type AdminAssignableRole = Exclude<TeamRole, 'owner'>

/** Detail payload returned by GET /admin/teams/:id */
export interface AdminTeamDetail {
  team: AdminTeam
  members: AdminTeamMember[]
  invitations: TeamInvitation[]
}

/**
 * List all teams with pagination
 * @param params - Pagination and filters (page, page_size, search, status)
 * @param options - Optional request options (signal)
 * @returns Paginated list of teams
 */
export async function list(
  params?: {
    page?: number
    page_size?: number
    search?: string
    status?: 'active' | 'disabled'
  },
  options?: {
    signal?: AbortSignal
  }
): Promise<PaginatedResponse<AdminTeam>> {
  const { data } = await apiClient.get<PaginatedResponse<AdminTeam>>('/admin/teams', {
    params: {
      page: params?.page,
      page_size: params?.page_size,
      search: params?.search,
      status: params?.status
    },
    signal: options?.signal
  })
  return data
}

/**
 * Create a new team (platform admin), assigning an owner by user id or email
 * @param payload - Team name and owner identity (one of owner_user_id / owner_email required)
 * @returns Created team
 */
export async function create(
  payload: { name: string; owner_user_id?: number; owner_email?: string; concurrency?: number; rpm_limit?: number }
): Promise<{ team: AdminTeam }> {
  const { data } = await apiClient.post<{ team: AdminTeam }>('/admin/teams', payload)
  return data
}

/**
 * Get team detail by ID (team + members + invitations)
 * @param id - Team ID
 * @returns Team detail
 */
export async function get(id: number): Promise<AdminTeamDetail> {
  const { data } = await apiClient.get<AdminTeamDetail>(`/admin/teams/${id}`)
  return data
}

/**
 * Update a team (name and/or status)
 * @param id - Team ID
 * @param payload - Fields to update
 * @returns Updated team
 */
export async function update(
  id: number,
  payload: { name?: string; status?: 'active' | 'disabled'; concurrency?: number; rpm_limit?: number }
): Promise<{ team: AdminTeam }> {
  const { data } = await apiClient.patch<{ team: AdminTeam }>(`/admin/teams/${id}`, payload)
  return data
}

/**
 * Add a member to a team (by existing user_id or by email)
 * @param id - Team ID
 * @param payload - Member identity (user_id or email) and role
 * @returns Created member
 */
export async function addMember(
  id: number,
  payload: { user_id?: number; email?: string; role: AdminAssignableRole }
): Promise<{ member: AdminTeamMember }> {
  const { data } = await apiClient.post<{ member: AdminTeamMember }>(
    `/admin/teams/${id}/members`,
    payload
  )
  return data
}

/**
 * Update a team member's role and/or status
 * @param id - Team ID
 * @param userId - Member user ID
 * @param payload - Fields to update
 * @returns Updated member
 */
export async function updateMember(
  id: number,
  userId: number,
  payload: { role?: AdminAssignableRole; status?: 'active' | 'suspended' }
): Promise<{ member: AdminTeamMember }> {
  const { data } = await apiClient.patch<{ member: AdminTeamMember }>(
    `/admin/teams/${id}/members/${userId}`,
    payload
  )
  return data
}

/**
 * Remove a member from a team
 * @param id - Team ID
 * @param userId - Member user ID
 * @returns Success confirmation
 */
export async function removeMember(id: number, userId: number): Promise<{ message: string }> {
  const { data } = await apiClient.delete<{ message: string }>(
    `/admin/teams/${id}/members/${userId}`
  )
  return data
}

/** Delete a team (platform admin force-delete). */
export async function deleteTeam(id: number): Promise<{ message: string }> {
  const { data } = await apiClient.delete<{ message: string }>(`/admin/teams/${id}`)
  return data
}

/**
 * Transfer team ownership to another member (platform admin)
 * @param id - Team ID
 * @param userId - User ID of the new owner (must be an existing member)
 * @returns Updated team payload
 */
export async function transferOwnership(id: number, userId: number): Promise<unknown> {
  const { data } = await apiClient.post<unknown>(
    `/admin/teams/${id}/transfer-ownership`,
    { user_id: userId }
  )
  return data
}

export const adminTeamsAPI = {
  list,
  get,
  create,
  update,
  addMember,
  updateMember,
  removeMember,
  deleteTeam,
  transferOwnership
}

export default adminTeamsAPI
