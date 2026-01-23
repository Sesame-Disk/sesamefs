import React from 'react';
import PropTypes from 'prop-types';
import { Form } from 'reactstrap';
import { gettext, siteRoot, mediaUrl } from '../../utils/constants';
import { seafileAPI } from '../../utils/seafile-api';

import '../../css/lib-decrypt.css';

const propTypes = {
  repoID: PropTypes.string.isRequired,
  onLibDecryptDialog: PropTypes.func.isRequired
};


class LibDecryptDialog extends React.Component {
  constructor(props) {
    super(props);
    this.state = {
      password: '',
      showError: false,
    };
    this.passwordInput = React.createRef();
  }

  componentDidMount() {
    // Focus password input after mount (replaces Modal's onOpened)
    setTimeout(() => {
      if (this.passwordInput.current) {
        this.passwordInput.current.focus();
      }
    }, 100);
  }

  handleSubmit = (e) => {
    let repoID = this.props.repoID;
    let password = this.state.password;
    seafileAPI.setRepoDecryptPassword(repoID, password).then(res => {
      this.props.onLibDecryptDialog();
    }).catch(res => {
      this.setState({
        showError: true
      });
    });

    e.preventDefault();
  };

  handleKeyDown = (e) => {
    if (e.key == 'Enter') {
      this.handleSubmit(e);
    }
  };

  handleChange = (e) => {
    this.setState({
      password: e.target.value,
      showError: false
    });
  };

  toggle = () => {
    this.props.onLibDecryptDialog();
  };

  render() {
    return (
      <div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
        <div className="modal-dialog modal-dialog-centered">
          <div className="modal-content">
            <div className="modal-header">
              <h5 className="modal-title">{gettext('This library is password protected')}</h5>
              <button type="button" className="close" onClick={this.toggle} aria-label="Close">
                <span aria-hidden="true">&times;</span>
              </button>
            </div>
            <div className="modal-body">
              <Form className="lib-decrypt-form text-center">
                <img src={`${mediaUrl}img/lock.svg`} alt="" aria-hidden="true" width="64" height="64" />
                <p className="intro">{gettext('This library is password protected')}</p>
                {this.state.showError &&
                  <p className="error">{gettext('Wrong password')}</p>
                }
                <input
                  type="password"
                  name="password"
                  className="form-control password-input"
                  autoComplete="off"
                  onKeyDown={this.handleKeyDown}
                  placeholder={gettext('Password')}
                  onChange={this.handleChange}
                  ref={this.passwordInput}
                />
                <button type="submit" className="btn btn-primary submit" onClick={this.handleSubmit}>{gettext('Submit')}</button>
                <p className="tip">{'* '}{gettext('The password will be kept in the server for only 1 hour.')}</p>
              </Form>
            </div>
          </div>
        </div>
      </div>
    );
  }
}

LibDecryptDialog.propTypes = propTypes;

export default LibDecryptDialog;
