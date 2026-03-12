/* eslint-disable */
import React, { useState, useRef, useEffect } from 'react';
import { gettext } from '../utils/constants';
import { FormGroup } from 'reactstrap';
import ShareLinkPanel from './share-link-panel';
import RenderShareButtons from './share-social-media';
import ChinaShareInfo from './china-share-info';
import './css.css';

function GenerateShareLinkWithPassword(props) {
  const [show, setShow] = useState(false);
  const panelRef = useRef(null);

  // Contar solo los links de China (que tienen password)
  const chinaLinksCount = (props.shareLinks || []).filter(link => link.has_password || (link.password && link.password !== '')).length;

  // Determinar el texto del botón según si hay links o no
  const getButtonText = () => {
    if (show) {
      return '▼ Hide';
    }
    if (chinaLinksCount === 0) {
      return '▶ Create Password-Protected Link';
    }
    if (chinaLinksCount === 1) {
      return `▶ View Link (${chinaLinksCount})`;
    }
    return `▶ View Links (${chinaLinksCount})`;
  };

  // Manejar el click del botón con lógica inteligente
  const handleToggle = (e) => {
    e.preventDefault();

    if (chinaLinksCount === 0 && !show) {
      // Si no hay links, abrir panel Y activar modo creación
      setShow(true);
      // Usar setTimeout para asegurarse de que el panel se ha renderizado
      setTimeout(() => {
        if (panelRef.current && panelRef.current.setMode) {
          panelRef.current.setMode('singleLinkCreation');
        }
      }, 0);
    } else {
      // Si hay links o ya está abierto, solo expandir/colapsar
      setShow(!show);
    }
  };

  return (
    <div className="china-share-wrapper">
      {/* Alert informativo de China */}
      {chinaLinksCount === 0 && <ChinaShareInfo />}

      {/* Título simple para China */}
      <div className="china-share-header">
        <div className="china-share-title">
          <span className="china-flag-icon">🇨🇳</span>
          <span className="china-title-text">
            {gettext('Share in China')}
            <small style={{ display: 'block', fontSize: '11px', opacity: 0.7, fontWeight: 'normal', marginTop: '2px' }}>
              🔍 {gettext('Filtered view: password-protected links')}
            </small>
          </span>
        </div>
        <button
          className="china-toggle-btn"
          onClick={handleToggle}
          aria-expanded={show}
        >
          {getButtonText()}
        </button>
      </div>

      {/* Panel de generación de links para China */}
      {show && (
        <div className="china-share-panel">
          {chinaLinksCount === 0 && (
            <div style={{
              padding: '12px',
              background: '#f0f8ff',
              borderRadius: '4px',
              marginBottom: '12px',
              fontSize: '13px',
              color: '#666'
            }}>
              ℹ️ {gettext('Create your first password-protected link to share with users in China')}
            </div>
          )}
          <ShareLinkPanel {...props} hideBatchButton={true} ref={panelRef} />
        </div>
      )}

      {/* Redes sociales */}
      <FormGroup className="mb-0 mt-3">
        <dt className="text-secondary font-weight-normal">{gettext('Share in social media:')}</dt>
        <dd><RenderShareButtons {...props} /></dd>
      </FormGroup>
    </div>
  );
}

export default GenerateShareLinkWithPassword;
