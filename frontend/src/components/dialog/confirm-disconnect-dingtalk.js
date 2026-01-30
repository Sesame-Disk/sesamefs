import React, { Component } from 'react';
import PropTypes from 'prop-types';
import { Button } from 'reactstrap';
import { gettext } from '../../utils/constants';

const propTypes = {
  formActionURL: PropTypes.string.isRequired,
  csrfToken: PropTypes.string.isRequired,
  toggle: PropTypes.func.isRequired
};

class ConfirmDisconnectDingtalk extends Component {

  constructor(props) {
    super(props);
    this.form = React.createRef();
  }

  disconnect = () => {
    this.form.current.submit();
  };

  render() {
    const {formActionURL, csrfToken, toggle} = this.props;
    return (
      <div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
          <div className="modal-dialog modal-dialog-centered">
            <div className="modal-content">
        <div className="modal-header">
              <h5 className="modal-title">{gettext('Disconnect')}</h5>
              <button type="button" className="close" onClick={toggle} aria-label="Close">
                <span aria-hidden="true">&times;</span>
              </button>
            </div>
        <div className="modal-body">
          <p>{gettext('Are you sure you want to disconnect?')}</p>
          <form ref={this.form} className="d-none" method="post" action={formActionURL}>
            <input type="hidden" name="csrfmiddlewaretoken" value={csrfToken} />
          </form>
        </div>
        <div className="modal-footer">
          <Button color="secondary" onClick={toggle}>{gettext('Cancel')}</Button>
          <Button color="primary" onClick={this.disconnect}>{gettext('Disconnect')}</Button>
        </div>
      </div>
          </div>
        </div>
    );
  }
}

ConfirmDisconnectDingtalk.propTypes = propTypes;

export default ConfirmDisconnectDingtalk;
