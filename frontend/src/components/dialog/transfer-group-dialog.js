import React from 'react';
import PropTypes from 'prop-types';
import { Button } from 'reactstrap';
import { gettext } from '../../utils/constants';
import { seafileAPI } from '../../utils/seafile-api';
import UserSelect from '../user-select';
import { Utils } from '../../utils/utils';

import '../../css/transfer-group-dialog.css';

const propTypes = {
  groupID: PropTypes.string,
  toggleTransferGroupDialog: PropTypes.func.isRequired,
  onGroupChanged: PropTypes.func.isRequired
};

class TransferGroupDialog extends React.Component {

  constructor(props) {
    super(props);
    this.state = {
      selectedOption: null,
      errMessage: '',
    };
    this.options = [];
  }

  handleSelectChange = (option) => {
    this.setState({
      selectedOption: option,
      errMessage: '',
    });
    this.options = [];
  };

  transferGroup = () => {
    const email = this.state.selectedOption && this.state.selectedOption.email;
    if (email) {
      seafileAPI.transferGroup(this.props.groupID, email).then((res) => {
        this.props.toggleTransferGroupDialog();
      }).catch((error) => {
        let errMessage = Utils.getErrorMsg(error);
        this.setState({errMessage: errMessage});
      });
    }
  };

  toggle = () => {
    this.props.toggleTransferGroupDialog();
  };

  render() {
    return (
      <div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
          <div className="modal-dialog modal-dialog-centered">
            <div className="modal-content">
        <div className="modal-header">
              <h5 className="modal-title">{gettext('Transfer Group')}</h5>
              <button type="button" className="close" onClick={this.toggle} aria-label="Close">
                <span aria-hidden="true">&times;</span>
              </button>
            </div>
        <div className="modal-body">
          <p>{gettext('Transfer group to')}</p>
          <UserSelect
            ref="userSelect"
            isMulti={false}
            className="reviewer-select"
            placeholder={gettext('Please enter 1 or more character')}
            onSelectChange={this.handleSelectChange}
          />
          <div className="error">{this.state.errMessage}</div>
        </div>
        <div className="modal-footer">
          <Button color="secondary" onClick={this.toggle}>{gettext('Close')}</Button>
          <Button color="primary" onClick={this.transferGroup}>{gettext('Submit')}</Button>
        </div>
      </div>
          </div>
        </div>
    );
  }
}

TransferGroupDialog.propTypes = propTypes;

export default TransferGroupDialog;
