import React, { Fragment } from 'react';
import PropTypes from 'prop-types';
import { Alert, FormGroup, Input, Label } from 'reactstrap';
import { gettext } from '../../../utils/constants';
import Loading from '../../loading';
import OpIcon from '../../op-icon';

const propTypes = {
  mode: PropTypes.string,
  permission: PropTypes.object,
  onChangeMode: PropTypes.func.isRequired,
  onUpdateCustomPermission: PropTypes.func.isRequired,
};

class CustomPermissionEditor extends React.Component {

  static defaultProps = {
    mode: 'add'
  };

  constructor(props) {
    super(props);
    this.state = {
      isLoading: true,
      permission_name: '',
      permission_desc: '',
      permission: {
        upload: false,
        download: false,
        create: false,
        modify: false,
        copy: false,
        delete: false,
        preview: false,
        download_external_link: false,
      },
      errMessage: '',
    };
  }

  componentDidMount() {
    const { permission } = this.props;
    if (permission) {
      this.setState({
        permission_name: permission.name,
        permission_desc: permission.description,
        permission: permission.permission,
        isLoading: false
      });
    } else {
      this.setState({isLoading: false});
    }

  }

  onChangePermissionName = (evt) => {
    const { permission_name } = this.state;
    const newName = evt.target.value;
    if (newName === permission_name) return;
    this.setState({permission_name: newName});
  };

  onChangePermissionDescription = (evt) => {
    const { permission_desc } = this.state;
    const newDescription = evt.target.value;
    if (newDescription === permission_desc) return;
    this.setState({permission_desc: newDescription});
  };

  onChangePermission = (type) => {
    return () => {
      const { permission } = this.state;
      const value = !permission[type];
      const newPermission = Object.assign({}, permission, {[type]: value});
      this.setState({permission: newPermission});
    };
  };

  validParams = () => {
    const { permission_name, permission_desc, permission } = this.state;
    let isValid = false;
    let errMessage = '';
    if (!permission_name || !permission_name.trim()) {
      errMessage = gettext('Name is required');
      return { isValid, errMessage };
    }
    if (!permission_desc || !permission_desc.trim()) {
      errMessage = gettext('Description is required');
      return { isValid, errMessage };
    }

    // Check that at least one permission option is selected
    const hasAnyPermission = Object.values(permission).some(v => v === true);
    if (!hasAnyPermission) {
      errMessage = gettext('Please select at least one permission option.');
      return { isValid, errMessage };
    }

    isValid = true;
    return { isValid };
  };

  onUpdateCustomPermission = () => {
    const { permission_name, permission_desc, permission } = this.state;
    const { isValid, errMessage } = this.validParams();
    if (!isValid) {
      this.setState({errMessage});
      return;
    }
    this.props.onUpdateCustomPermission(permission_name, permission_desc, permission);
  };

  render() {

    const { mode } = this.props;
    const title = mode === 'add' ? gettext('Add permission') : gettext('Edit permission');

    const { isLoading, permission_name, permission_desc, permission, errMessage } = this.state;

    return (
      <div className="custom-permission">
        <div className="permission-header">
          <div className="d-flex align-items-center">
            <OpIcon
              className="fa fa-arrow-left back-icon"
              op={this.props.onChangeMode}
              title={gettext('Back')}
            />
            <span>{title}</span>
          </div>
          <div className="operation">
            <button type="button" className="btn btn-sm btn-outline-primary" onClick={this.onUpdateCustomPermission}>{gettext('Submit')}</button>
          </div>
        </div>
        <div className="permission-main mt-4">
          {isLoading && <Loading />}
          {!isLoading && (
            <Fragment>
              <div className="permission-name-desc d-flex">
                <FormGroup className="permission-name">
                  <Label>{gettext('Permission name')}</Label>
                  <Input value={permission_name || ''} onChange={this.onChangePermissionName} />
                </FormGroup>
                <FormGroup className="permission-desc">
                  <Label>{gettext('Description')}</Label>
                  <Input value={permission_desc || ''} onChange={this.onChangePermissionDescription} />
                </FormGroup>
              </div>
              {errMessage && <Alert color="danger" fade={false} className="mt-2 mb-2 custom-permission-error">{errMessage}</Alert>}
              <div className="permission-options mt-2">
                <FormGroup check>
                  <Label check>
                    <Input type="checkbox" onChange={this.onChangePermission('upload')} checked={permission.upload}/>
                    <span>{gettext('Upload')}</span>
                  </Label>
                </FormGroup>
                <FormGroup check>
                  <Label check>
                    <Input type="checkbox" onChange={this.onChangePermission('download')} checked={permission.download}/>
                    <span>{gettext('Download')}</span>
                  </Label>
                </FormGroup>
                <FormGroup check>
                  <Label check>
                    <Input type="checkbox" onChange={this.onChangePermission('create')} checked={permission.create}/>
                    <span>{gettext('Create')}</span>
                  </Label>
                </FormGroup>
                <FormGroup check className="d-flex align-items-center">
                  <Label check>
                    <Input type="checkbox" onChange={this.onChangePermission('modify')} checked={permission.modify}/>
                    <span>{gettext('Modify')}</span>
                  </Label>
                  <span className="fa fa-question-circle ml-2 modify-tip-wrapper" style={{color: '#999', cursor: 'pointer'}}>
                    <span className="modify-tip-text">{gettext('Modify includes modify file, move/rename file and folder')}</span>
                  </span>
                </FormGroup>
                <FormGroup check>
                  <Label check>
                    <Input type="checkbox" onChange={this.onChangePermission('copy')} checked={permission.copy}/>
                    <span>{gettext('Copy')}</span>
                  </Label>
                </FormGroup>
                <FormGroup check>
                  <Label check>
                    <Input type="checkbox" onChange={this.onChangePermission('delete')} checked={permission.delete}/>
                    <span>{gettext('Delete')}</span>
                  </Label>
                </FormGroup>
                <FormGroup check>
                  <Label check>
                    <Input type="checkbox" onChange={this.onChangePermission('preview')} checked={permission.preview}/>
                    <span>{gettext('Preview online')}</span>
                  </Label>
                </FormGroup>
                <FormGroup check>
                  <Label check>
                    <Input type="checkbox" onChange={this.onChangePermission('download_external_link')} checked={permission.download_external_link}/>
                    <span>{gettext('Generate share link')}</span>
                  </Label>
                </FormGroup>
              </div>
            </Fragment>
          )}
        </div>
      </div>
    );
  }

}

CustomPermissionEditor.propTypes = propTypes;

export default CustomPermissionEditor;
