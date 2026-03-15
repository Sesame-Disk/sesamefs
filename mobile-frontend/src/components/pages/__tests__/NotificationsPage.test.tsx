import React from 'react';
import { render, screen, waitFor, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import NotificationsPage from '../NotificationsPage';

const mockNotifications = [
  {
    id: 1,
    type: 'repo_share',
    detail: {
      share_from_user_name: 'Alice',
      repo_name: 'Documents',
      path: '/',
    },
    seen: false,
    timestamp: '2026-03-15T10:00:00Z',
    msg_from: 'alice@example.com',
  },
  {
    id: 2,
    type: 'add_user_to_group',
    detail: {
      group_staff_name: 'Bob',
      group_name: 'Engineering',
    },
    seen: true,
    timestamp: '2026-03-14T09:00:00Z',
    msg_from: 'bob@example.com',
  },
  {
    id: 3,
    type: 'file_uploaded',
    detail: {
      file_name: 'report.pdf',
      folder_name: 'Reports',
    },
    seen: false,
    timestamp: '2026-03-13T15:00:00Z',
    msg_from: 'carol@example.com',
  },
];

vi.mock('../../../lib/api', () => ({
  listNotifications: vi.fn(),
  markNotificationAsRead: vi.fn(),
  markAllNotificationsRead: vi.fn(),
  deleteNotification: vi.fn(),
}));

// Mock framer-motion to avoid animation issues in tests
vi.mock('framer-motion', () => ({
  motion: {
    div: React.forwardRef(({ children, animate, transition, drag, dragConstraints, dragElastic, onDragEnd, ...props }: any, ref: any) =>
      React.createElement('div', { ...props, ref }, children)
    ),
  },
  AnimatePresence: ({ children }: any) => children,
}));

import {
  listNotifications,
  markNotificationAsRead,
  markAllNotificationsRead,
  deleteNotification,
} from '../../../lib/api';

const mockListNotifications = vi.mocked(listNotifications);
const mockMarkNotificationAsRead = vi.mocked(markNotificationAsRead);
const mockMarkAllNotificationsRead = vi.mocked(markAllNotificationsRead);
const mockDeleteNotification = vi.mocked(deleteNotification);

// Mock IntersectionObserver
const mockObserve = vi.fn();
const mockDisconnect = vi.fn();

beforeEach(() => {
  vi.clearAllMocks();
  const MockIntersectionObserver = vi.fn().mockImplementation(() => ({
    observe: mockObserve,
    disconnect: mockDisconnect,
    unobserve: vi.fn(),
  }));
  vi.stubGlobal('IntersectionObserver', MockIntersectionObserver);
});

describe('NotificationsPage', () => {
  it('shows loading state initially', () => {
    mockListNotifications.mockReturnValue(new Promise(() => {}));
    render(<NotificationsPage />);
    expect(screen.getByTestId('loading')).toBeInTheDocument();
  });

  it('renders notification items after loading', async () => {
    mockListNotifications.mockResolvedValue({
      notification_list: mockNotifications,
      count: 3,
    });
    render(<NotificationsPage />);

    await waitFor(() => {
      expect(screen.getByTestId('notifications-page')).toBeInTheDocument();
    });

    const items = screen.getAllByTestId('notification-item');
    expect(items).toHaveLength(3);
  });

  it('shows empty state when no notifications', async () => {
    mockListNotifications.mockResolvedValue({
      notification_list: [],
      count: 0,
    });
    render(<NotificationsPage />);

    await waitFor(() => {
      expect(screen.getByText('No notifications')).toBeInTheDocument();
    });
  });

  it('marks a notification as read on click', async () => {
    mockListNotifications.mockResolvedValue({
      notification_list: mockNotifications,
      count: 3,
    });
    mockMarkNotificationAsRead.mockResolvedValue(undefined);
    render(<NotificationsPage />);

    await waitFor(() => {
      expect(screen.getByTestId('notifications-page')).toBeInTheDocument();
    });

    const items = screen.getAllByTestId('notification-item');
    await act(async () => {
      items[0].click();
    });

    expect(mockMarkNotificationAsRead).toHaveBeenCalledWith(1);
  });

  it('marks all notifications as read', async () => {
    mockListNotifications.mockResolvedValue({
      notification_list: mockNotifications,
      count: 3,
    });
    mockMarkAllNotificationsRead.mockResolvedValue(undefined);
    render(<NotificationsPage />);

    await waitFor(() => {
      expect(screen.getByTestId('notifications-page')).toBeInTheDocument();
    });

    const markAllBtn = screen.getByTestId('mark-all-read-btn');
    await act(async () => {
      markAllBtn.click();
    });

    expect(mockMarkAllNotificationsRead).toHaveBeenCalled();
  });

  it('shows unread indicators for unseen notifications', async () => {
    mockListNotifications.mockResolvedValue({
      notification_list: mockNotifications,
      count: 3,
    });
    render(<NotificationsPage />);

    await waitFor(() => {
      expect(screen.getByTestId('notifications-page')).toBeInTheDocument();
    });

    const unreadIndicators = screen.getAllByTestId('unread-indicator');
    // notifications[0] and notifications[2] are unseen
    expect(unreadIndicators).toHaveLength(2);
  });

  it('displays correct notification messages', async () => {
    mockListNotifications.mockResolvedValue({
      notification_list: mockNotifications,
      count: 3,
    });
    render(<NotificationsPage />);

    await waitFor(() => {
      expect(screen.getByTestId('notifications-page')).toBeInTheDocument();
    });

    expect(screen.getByText('Alice shared library "Documents" with you')).toBeInTheDocument();
    expect(screen.getByText('Bob added you to group "Engineering"')).toBeInTheDocument();
    expect(screen.getByText('File "report.pdf" uploaded to "Reports"')).toBeInTheDocument();
  });

  it('renders scroll sentinel when there are more items', async () => {
    const fullPage = Array.from({ length: 25 }, (_, i) => ({
      ...mockNotifications[0],
      id: i + 1,
    }));
    mockListNotifications.mockResolvedValue({
      notification_list: fullPage,
      count: 50,
    });
    render(<NotificationsPage />);

    await waitFor(() => {
      expect(screen.getByTestId('notifications-page')).toBeInTheDocument();
    });

    expect(screen.getByTestId('scroll-sentinel')).toBeInTheDocument();
  });

  it('calls listNotifications with page 1 on mount', async () => {
    mockListNotifications.mockResolvedValue({
      notification_list: mockNotifications,
      count: 3,
    });
    render(<NotificationsPage />);

    await waitFor(() => {
      expect(screen.getByTestId('notifications-page')).toBeInTheDocument();
    });

    expect(mockListNotifications).toHaveBeenCalledWith(1, 25);
  });
});
