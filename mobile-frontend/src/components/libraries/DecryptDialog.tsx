import React, { useState, useEffect } from 'react';
import { Lock, Eye, EyeOff } from 'lucide-react';
import BottomSheet from '../ui/BottomSheet';
import { setRepoPassword } from '../../lib/api';
import {
  getRememberedPassword,
  setRememberedPassword,
  markRepoDecrypted,
} from '../../lib/encryption';

interface DecryptDialogProps {
  isOpen: boolean;
  onClose: () => void;
  repoId: string;
  repoName: string;
  onSuccess: () => void;
}

export default function DecryptDialog({ isOpen, onClose, repoId, repoName, onSuccess }: DecryptDialogProps) {
  const [password, setPassword] = useState('');
  const [showPassword, setShowPassword] = useState(false);
  const [remember, setRemember] = useState(false);
  const [error, setError] = useState('');
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (!isOpen) return;
    setPassword('');
    setShowPassword(false);
    setError('');
    setSubmitting(false);

    const remembered = getRememberedPassword(repoId);
    if (remembered) {
      setPassword(remembered);
      setRemember(true);
      submitPassword(remembered);
    }
  }, [isOpen, repoId]);

  async function submitPassword(pwd: string) {
    setSubmitting(true);
    setError('');
    try {
      await setRepoPassword(repoId, pwd);
      markRepoDecrypted(repoId);
      if (remember) {
        setRememberedPassword(repoId, pwd);
      }
      onSuccess();
      onClose();
    } catch {
      setError('Incorrect password');
    } finally {
      setSubmitting(false);
    }
  }

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!password) return;
    submitPassword(password);
  };

  return (
    <BottomSheet isOpen={isOpen} onClose={onClose} title="Encrypted Library">
      <form onSubmit={handleSubmit} className="flex flex-col items-center gap-4">
        <Lock className="w-12 h-12 text-primary" />
        <p className="text-text font-medium">This library is encrypted</p>
        <p className="text-gray-500 text-sm text-center">
          Enter password to access {repoName}
        </p>

        <div className="w-full relative">
          <input
            type={showPassword ? 'text' : 'password'}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            placeholder="Password"
            className="w-full border border-gray-300 rounded-lg px-4 py-3 pr-12 text-base min-h-[44px]"
            autoFocus
            data-testid="decrypt-password-input"
          />
          <button
            type="button"
            onClick={() => setShowPassword(!showPassword)}
            className="absolute right-2 top-1/2 -translate-y-1/2 min-h-[44px] min-w-[44px] flex items-center justify-center"
            aria-label={showPassword ? 'Hide password' : 'Show password'}
          >
            {showPassword ? (
              <EyeOff className="w-5 h-5 text-gray-400" />
            ) : (
              <Eye className="w-5 h-5 text-gray-400" />
            )}
          </button>
        </div>

        <label className="flex items-center gap-2 w-full cursor-pointer">
          <input
            type="checkbox"
            checked={remember}
            onChange={(e) => setRemember(e.target.checked)}
            className="w-5 h-5"
            data-testid="remember-checkbox"
          />
          <span className="text-sm text-text">Remember password</span>
        </label>

        {error && (
          <p className="text-red-500 text-sm w-full" role="alert" data-testid="decrypt-error">
            {error}
          </p>
        )}

        <button
          type="submit"
          disabled={!password || submitting}
          className="w-full bg-primary text-white rounded-lg py-3 min-h-[44px] font-medium disabled:opacity-50"
          data-testid="decrypt-submit"
        >
          {submitting ? 'Decrypting...' : 'Decrypt'}
        </button>
      </form>
    </BottomSheet>
  );
}
