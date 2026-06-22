import { apiClient } from './client'
import type { WorkspacesResponse, TeamMember, TeamInvitation, TeamRole } from '@/types'

/** Response of POST /teams/:id/invitations — invitation row plus the copyable accept link. */
export interface InviteMemberResponse {
  invitation: TeamInvitation
  token: string
  accept_link: string
}

/** Response of GET /teams/invitations/preview — read-only invitation summary (no mutation). */
export interface InvitationPreview {
  team_id: number
  team_name: string
  role: string
  email: string
  status: string
  expired: boolean
}

/** Response of POST /teams/invitations/accept — the joined team summary plus the new membership. */
export interface AcceptInvitationResponse {
  team: { id: number; name: string; billing_subject_id: number }
  member: TeamMember
}

export const workspacesAPI = {
  async list(): Promise<WorkspacesResponse> {
    const { data } = await apiClient.get<WorkspacesResponse>('/workspaces')
    return data
  },

  async createTeam(payload: { name: string }): Promise<{ team: { id: number; name: string; slug: string; billing_subject_id: number } }> {
    const { data } = await apiClient.post<{ team: { id: number; name: string; slug: string; billing_subject_id: number } }>('/teams', payload)
    return data
  },

  async listMembers(teamId: number): Promise<{ members: TeamMember[]; invitations: TeamInvitation[] }> {
    const { data } = await apiClient.get<{ members: TeamMember[]; invitations: TeamInvitation[] }>(`/teams/${teamId}/members`)
    return data
  },

  async inviteMember(teamId: number, payload: { email: string; role: Exclude<TeamRole, 'owner'> }): Promise<InviteMemberResponse> {
    const { data } = await apiClient.post<InviteMemberResponse>(`/teams/${teamId}/invitations`, payload)
    return data
  },

  async updateMember(teamId: number, userId: number, payload: { role?: TeamRole; status?: 'active' | 'suspended' }): Promise<TeamMember> {
    const { data } = await apiClient.patch<TeamMember>(`/teams/${teamId}/members/${userId}`, payload)
    return data
  },

  async removeMember(teamId: number, userId: number): Promise<{ message: string }> {
    const { data } = await apiClient.delete<{ message: string }>(`/teams/${teamId}/members/${userId}`)
    return data
  },

  async previewInvitation(token: string): Promise<InvitationPreview> {
    const { data } = await apiClient.get<InvitationPreview>('/teams/invitations/preview', {
      params: { token }
    })
    return data
  },

  async acceptInvitation(token: string): Promise<AcceptInvitationResponse> {
    const { data } = await apiClient.post<AcceptInvitationResponse>('/teams/invitations/accept', { token })
    return data
  },

  async transferOwnership(teamId: number, userId: number): Promise<unknown> {
    const { data } = await apiClient.post<unknown>(`/teams/${teamId}/transfer-ownership`, { user_id: userId })
    return data
  }
}
