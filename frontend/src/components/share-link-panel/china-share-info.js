import React, { useState, useEffect } from 'react';
import { gettext } from '../../utils/constants';

function ChinaShareInfo() {
    const STORAGE_KEY = 'chinaShareInfo_dismissed';
    const DISMISS_DURATION = 30 * 24 * 60 * 60 * 1000; // 30 days in milliseconds

    const [isDismissed, setIsDismissed] = useState(false);

    useEffect(() => {
        const dismissedData = localStorage.getItem(STORAGE_KEY);
        if (dismissedData) {
            const { timestamp } = JSON.parse(dismissedData);
            const now = new Date().getTime();
            if (now - timestamp < DISMISS_DURATION) {
                setIsDismissed(true);
            } else {
                localStorage.removeItem(STORAGE_KEY);
            }
        }
    }, []);

    const handleDismiss = () => {
        const dismissData = { timestamp: new Date().getTime() };
        localStorage.setItem(STORAGE_KEY, JSON.stringify(dismissData));
        setIsDismissed(true);
    };

    if (isDismissed) {
        return null;
    }

    return (
        <div className="china-info-alert">
            <div className="china-info-alert-content">
                <button
                    onClick={handleDismiss}
                    className="china-info-alert-close-btn"
                >
                    ×
                </button>
                <strong>{gettext('Sharing to China?')}</strong>
                <p>
                    {gettext('Share links WITH a password are accessible worldwide, including China (no VPN needed). Links without a password work everywhere except China.')}
                </p>
                <p style={{ marginTop: '8px', fontSize: '11px', opacity: 0.9 }}>
                    <strong>{gettext('Tip:')}</strong> {gettext('Add a password to any share link to make it accessible in China without VPN.')}
                </p>
            </div>
        </div>
    );
}

export default ChinaShareInfo;
