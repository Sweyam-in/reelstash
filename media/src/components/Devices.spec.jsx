import React from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

vi.mock('../api', () => ({
  approveDevicePairing: vi.fn(), lookupDevicePairing: vi.fn(), getDevices: vi.fn(), revokeDevice: vi.fn(),
}));
import { approveDevicePairing, lookupDevicePairing } from '../api';
import { LinkDevicePage } from './Devices';
import { renderApp } from '../testing/render';

beforeEach(() => vi.clearAllMocks());

describe('device linking', () => {
  it('checks the code, asks for device confirmation, and shows completion', async () => {
    lookupDevicePairing.mockResolvedValue({ deviceName: 'Living room TV', platform: 'androidtv' });
    approveDevicePairing.mockResolvedValue({});
    renderApp(<LinkDevicePage />);

    const code = screen.getByLabelText('Code shown on your device');
    await userEvent.type(code, 'ab12cd34');
    expect(code).toHaveValue('AB12-CD34');
    await userEvent.click(screen.getByRole('button', { name: /Continue/ }));
    await waitFor(() => expect(lookupDevicePairing).toHaveBeenCalledWith('AB12-CD34'));
    expect(await screen.findByText('Living room TV')).toBeInTheDocument();
    expect(approveDevicePairing).not.toHaveBeenCalled();

    await userEvent.click(screen.getByRole('button', { name: 'Approve and link device' }));
    await waitFor(() => expect(approveDevicePairing).toHaveBeenCalledWith('AB12-CD34'));
    expect(await screen.findByText('Living room TV is connected')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Link another device' }));
    expect(screen.getByLabelText('Code shown on your device')).toHaveValue('');
  });

  it('lets the person correct a code after lookup fails', async () => {
    lookupDevicePairing.mockRejectedValueOnce(new Error('Code expired'));
    renderApp(<LinkDevicePage />);
    const code = screen.getByLabelText('Code shown on your device');
    await userEvent.type(code, 'AB12CD34');
    await userEvent.click(screen.getByRole('button', { name: /Continue/ }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Code expired');
    await userEvent.clear(code);
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });
});
