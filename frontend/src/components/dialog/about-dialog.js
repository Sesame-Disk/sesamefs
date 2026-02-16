import React from 'react';
import PropTypes from 'prop-types';

import { gettext, lang, mediaUrl, logoPath, logoWidth, logoHeight, siteTitle, seafileVersion, additionalAboutDialogLinks, aboutDialogCustomHtml } from '../../utils/constants';

const propTypes = {
  onCloseAboutDialog: PropTypes.func.isRequired,
};

class AboutDialog extends React.Component {

  renderExternalAboutLinks = () => {
    if (additionalAboutDialogLinks && (typeof additionalAboutDialogLinks) === 'object') {
      let keys = Object.keys(additionalAboutDialogLinks);
      return keys.map((key, index) => {
        return <a key={index} className="d-block" href={additionalAboutDialogLinks[key]}>{key}</a>;
      });
    }
    return null;
  };

  render() {
    // let href = lang === 'zh-cn' ? 'http://seafile.com/about/' : 'http://seafile.com/en/about/';
    let href = "https://sesamedisk.com";
    const { onCloseAboutDialog: toggleDialog } = this.props;

    if (aboutDialogCustomHtml) {
      return (
        <div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
          <div className="modal-dialog modal-dialog-centered">
            <div className="modal-content">
          <div className="modal-body">
            <button type="button" className="close" onClick={toggleDialog}><span aria-hidden="true">×</span></button>
            <div className="about-content" dangerouslySetInnerHTML={{ __html: aboutDialogCustomHtml }}></div>
          </div>
        </div>
          </div>
        </div>
      );
    } else {
      return (
        <div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
          <div className="modal-dialog modal-dialog-centered">
            <div className="modal-content">
          <div className="modal-body">
            <button type="button" className="close" onClick={toggleDialog}><span aria-hidden="true">×</span></button>
            <div className="about-content">
              <p><img src={mediaUrl + logoPath} height={logoHeight} style={{width: 'auto'}} title={siteTitle} alt="logo" /></p>
              <p>SesameFS by Sesame Disk LLC</p>
              <p>{gettext('Server Version: ')}{seafileVersion}<br />© {(new Date()).getFullYear()} Sesame Disk LLC</p>
              <p>{this.renderExternalAboutLinks()}</p>
              <p><a href={href} target="_blank" rel="noreferrer">{gettext('About Us')}</a></p>
            </div>
          </div>
        </div>
          </div>
        </div>
      );
    }
  }
}

AboutDialog.propTypes = propTypes;

export default AboutDialog;
