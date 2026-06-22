import { apiClient } from './client'
import type { WorkspacesResponse, TeamMember, TeamInvitation, TeamRole } from '@/types'

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

  async inviteMember(teamId: number, payload: { email: string; role: Exclude<TeamRole, 'owner'> }): Promise<TeamInvitation> {
    const { data } = await apiClient.post<TeamInvitation>(`/teams/${teamId}/invitations`, payload)
    return data
  },

  async updateMember(teamId: number, userId: number, payload: { role?: TeamRole; status?: 'active' | 'suspended' }): Promise<TeamMember> {
    const { data } = await apiClient.patch<TeamMember>(`/teams/${teamId}/members/${userId}`, payload)
    return data
  },

  async removeMember(teamId: number, userId: number): Promise<{ message: string }> {
    const { data } = await apiClient.delete<{ message: string }>(`/teams/${teamId}/members/${userId}`)
    return data
  }
}
